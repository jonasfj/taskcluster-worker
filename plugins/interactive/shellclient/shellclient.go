package shellclient

import (
	"encoding/binary"
	"io"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/walac/taskcluster-worker/engines"
	"github.com/walac/taskcluster-worker/plugins/interactive/shellconsts"
	"github.com/walac/taskcluster-worker/runtime/atomics"
	"github.com/walac/taskcluster-worker/runtime/ioext"
)

// ShellClient exposes the client interface to a shell running remotely.
// This object implements the engines.Shell interface.
type ShellClient struct {
	ws           *websocket.Conn
	mWrite       sync.Mutex
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	stdinReader  *ioext.PipeReader
	stdoutWriter io.WriteCloser
	stderrWriter io.WriteCloser
	resolve      atomics.Once // Must wrap access to success/err
	success      bool
	err          error
	done         chan struct{} // Closed when success/err is ready
}

// New takes a websocket and creates a ShellClient object implementing the
// engines.Shell interface.
func New(ws *websocket.Conn) *ShellClient {
	stdinReader, stdin := ioext.BlockedPipe()
	tellOut := make(chan int, 10)
	tellErr := make(chan int, 10)
	stdout, stdoutWriter := ioext.AsyncPipe(shellconsts.ShellMaxPendingBytes, tellOut)
	stderr, stderrWriter := ioext.AsyncPipe(shellconsts.ShellMaxPendingBytes, tellErr)
	stdinReader.Unblock(shellconsts.ShellMaxPendingBytes)

	s := &ShellClient{
		ws:           ws,
		stdin:        stdin,
		stdout:       stdout,
		stderr:       stderr,
		stdinReader:  stdinReader,
		stdoutWriter: stdoutWriter,
		stderrWriter: stderrWriter,
		done:         make(chan struct{}),
	}

	ws.SetReadLimit(shellconsts.ShellMaxMessageSize)
	ws.SetReadDeadline(time.Now().Add(shellconsts.ShellPongTimeout))
	ws.SetPongHandler(s.pongHandler)

	go s.writeMessages()
	go s.readMessages()
	go s.sendPings()
	go s.sendAck(shellconsts.StreamStdout, tellOut)
	go s.sendAck(shellconsts.StreamStderr, tellErr)

	return s
}

func (s *ShellClient) dispose() {
	// Signal that we're done
	select {
	case <-s.done:
	default:
		close(s.done)
	}

	// Close websocket
	s.ws.Close()

	// Close all streams
	s.stdinReader.Close()
	s.stdoutWriter.Close()
	s.stderrWriter.Close()
}

func (s *ShellClient) send(message []byte) bool {
	// Write message and ensure we reset the write deadline
	s.mWrite.Lock()
	s.ws.SetWriteDeadline(time.Now().Add(shellconsts.ShellWriteTimeout))
	err := s.ws.WriteMessage(websocket.BinaryMessage, message)
	s.mWrite.Unlock()

	if err != nil {
		s.resolve.Do(func() {
			debug("Resolving internal error: Failed to send message, error: %s", err)
			s.success = false
			s.err = engines.ErrNonFatalInternalError
			s.dispose()
		})
		return false
	}
	return true
}

func (s *ShellClient) sendPings() {
	for {
		// Sleep for ping interval time
		time.Sleep(shellconsts.ShellPingInterval)

		// Write a ping message, and reset the write deadline
		s.mWrite.Lock()
		s.ws.SetWriteDeadline(time.Now().Add(shellconsts.ShellWriteTimeout))
		err := s.ws.WriteMessage(websocket.PingMessage, []byte{})
		s.mWrite.Unlock()

		// If there is an error we resolve with internal error
		if err != nil {
			s.resolve.Do(func() {
				debug("Resolving with internal-error, failed to send ping, error: %s", err)
				s.success = false
				s.err = engines.ErrNonFatalInternalError
				s.dispose()
			})
			return
		}
	}
}

func (s *ShellClient) sendAck(streamID byte, tell <-chan int) {
	// reserve a buffer for sending acknowledgments
	ack := make([]byte, 2+4)
	ack[0] = shellconsts.MessageTypeAck
	var size int64

	for n := range tell {
		// Merge in as many tell message as is pending
		N := n
		for n > 0 {
			select {
			case n = <-tell:
				N += n
			default:
				n = 0
			}
		}
		// Record the size for logging
		size += int64(N)

		// Send an acknowledgment message (this is for congestion control)
		ack[1] = streamID
		binary.BigEndian.PutUint32(ack[2:], uint32(N))
		s.send(ack)
	}
	debug("Final ack for streamID: %d sent, size: %d", streamID, size)
}

func (s *ShellClient) pongHandler(string) error {
	// Reset the read deadline
	s.ws.SetReadDeadline(time.Now().Add(shellconsts.ShellPongTimeout))
	return nil
}

func (s *ShellClient) writeMessages() {
	m := make([]byte, 2+shellconsts.ShellBlockSize)
	m[0] = shellconsts.MessageTypeData
	m[1] = shellconsts.StreamStdin
	var size int64

	for {
		n, err := s.stdinReader.Read(m[2:])
		size += int64(n)

		// Send payload if more than zero (zero payload indicates end of stream)
		if n > 0 {
			s.send(m[:2+n])
		}

		// If EOF, then we send an empty payload to signal this
		if err == io.EOF {
			debug("Reached EOF of stdin, size: %d", size)
			s.send(m[:2])
			return
		}

		if err != nil && err != io.EOF {
			// If we fail to read from stdin, then we cleanup
			s.resolve.Do(func() {
				debug("Resolving internal error: Failed to read stdin, error: %s", err)
				s.success = false
				s.err = engines.ErrNonFatalInternalError
				s.dispose()
			})
			return
		}
	}
}

func (s *ShellClient) readMessages() {
	for {
		t, m, err := s.ws.ReadMessage()
		if err != nil {
			s.resolve.Do(func() {
				debug("Resolving internal error: Failed to read message, error: %s", err)
				s.success = false
				s.err = engines.ErrNonFatalInternalError
				s.dispose()
			})
			return
		}

		// Skip anything that isn't a binary message
		if t != websocket.BinaryMessage || len(m) == 0 {
			continue
		}

		// Find [type] and [data]
		mType := m[0]
		mData := m[1:]

		// If we get a datatype
		if mType == shellconsts.MessageTypeData && len(mData) > 0 {
			// Find [stream] and [payload]
			mStream := mData[0]
			mPayload := mData[1:]

			// Write payload or close stream if payload is zero length
			var err error
			if mStream == shellconsts.StreamStdout {
				if len(mPayload) > 0 {
					_, err = s.stdoutWriter.Write(mPayload)
				} else {
					err = s.stdoutWriter.Close()
				}
			}
			if mStream == shellconsts.StreamStderr {
				if len(mPayload) > 0 {
					_, err = s.stderrWriter.Write(mPayload)
				} else {
					err = s.stderrWriter.Close()
				}
			}

			// If there was an error writing to output stream we close with error
			if err != nil {
				s.resolve.Do(func() {
					debug("Resolving internal error: Failed to write streamID: %d, error: %s", mStream, err)
					s.success = false
					s.err = engines.ErrNonFatalInternalError
					s.dispose()
				})
				return
			}
		}

		// If bytes from stdin are acknowledged, then we unblock additional bytes
		if mType == shellconsts.MessageTypeAck && len(mData) == 5 {
			if mData[0] == shellconsts.StreamStdin {
				n := binary.BigEndian.Uint32(mData[1:])
				s.stdinReader.Unblock(int64(n))
			}
		}

		// If we get an exit message, we resolve and close the websocket
		if mType == shellconsts.MessageTypeExit && len(mData) == 1 {
			s.resolve.Do(func() {
				s.success = (mData[0] == 0)
				s.err = engines.ErrShellTerminated
				debug("Resolving due to Exit message, success: %v", s.success)

				s.mWrite.Lock()
				s.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				s.mWrite.Unlock()
				s.dispose()
			})
			return
		}
	}
}

// StdinPipe returns a pipe to which stdin must be written.
// It's important to close stdin, if you expect the remote shell to terminate.
func (s *ShellClient) StdinPipe() io.WriteCloser {
	return s.stdin
}

// StdoutPipe returns a pipe from which stdout must be read.
// It's important to drain this pipe or the shell will block when the internal
// buffer is full.
func (s *ShellClient) StdoutPipe() io.ReadCloser {
	return s.stdout
}

// StderrPipe returns a pipe from which stderr must be read.
// It's important to drain this pipe or the shell will block when the internal
// buffer is full.
func (s *ShellClient) StderrPipe() io.ReadCloser {
	return s.stderr
}

// SetSize will attempt to set the TTY width (columns) and height (rows) on the
// remote shell.
func (s *ShellClient) SetSize(columns, rows uint16) error {
	// Write a size message
	m := make([]byte, 5)
	m[0] = shellconsts.MessageTypeSize
	binary.BigEndian.PutUint16(m[1:], columns)
	binary.BigEndian.PutUint16(m[3:], rows)
	sent := s.send(m)

	// If we failed to send it, we check if we're done and return resolution
	// otherwise we have an internal error.
	if !sent {
		select {
		case <-s.done:
			return s.err
		default:
			return engines.ErrNonFatalInternalError
		}
	}

	return nil
}

// Abort will tell the remote shell to abort and close the websocket.
func (s *ShellClient) Abort() error {
	s.resolve.Do(func() {
		debug("Resolving by aborting shell")

		// Write an abort message
		m := make([]byte, 1)
		m[0] = shellconsts.MessageTypeAbort
		s.send(m)

		// Set success false, err to shell aborted
		s.success = false
		s.err = engines.ErrShellAborted

		// Close the websocket
		s.dispose()
	})

	s.resolve.Wait()
	if s.err == engines.ErrShellAborted {
		return nil
	}
	return s.err
}

// Wait will wait for the remote shell to finish, by either succeeding or
// returning an error.
func (s *ShellClient) Wait() (bool, error) {
	s.resolve.Wait()
	if s.err == engines.ErrShellTerminated {
		return s.success, nil
	}
	return s.success, s.err
}

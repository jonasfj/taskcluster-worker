package shellserver

import (
	"bytes"
	"io"
	"io/ioutil"
	"os/exec"

	"github.com/walac/taskcluster-worker/engines"
	"github.com/walac/taskcluster-worker/plugins/interactive/pty"
	"github.com/walac/taskcluster-worker/runtime/atomics"
	"github.com/walac/taskcluster-worker/runtime/ioext"
)

type execShell struct {
	cmd       *exec.Cmd
	pty       *pty.PTY
	resolve   atomics.Once
	result    bool
	resultErr error
	abortErr  error
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
}

func newExecShell(command []string, tty bool) (engines.Shell, error) {
	if len(command) == 0 {
		command = []string{defaultShell}
	}
	s := &execShell{
		cmd: exec.Command(command[0], command[1:]...),
	}

	// Start is wrapped in pty, if shell is supposed to emulate a TTY
	var err error
	if tty && pty.Supported {
		s.pty, err = pty.Start(s.cmd)
		if err != nil {
			// if there was a start error we set empty streams
			s.stdin = ioext.WriteNopCloser(ioutil.Discard)
			s.stdout = ioutil.NopCloser(bytes.NewReader(nil))
			s.stderr = ioutil.NopCloser(bytes.NewReader(nil))
		} else {
			s.stdin = s.pty
			s.stdout = s.pty
			s.stderr = ioutil.NopCloser(bytes.NewReader(nil))
		}
	} else {
		s.cmd.Stdin, s.stdin = io.Pipe()
		s.stdout, s.cmd.Stdout = io.Pipe()
		s.stderr, s.cmd.Stderr = io.Pipe()

		err = s.cmd.Start()
	}

	// if there was an error starting, then we just resolve as is... Hence, it'll
	// be empty stdio and false result.
	if err != nil {
		s.resolve.Do(func() {
			s.stdin.Close()
			s.stdout.Close()
			s.stderr.Close()

			s.result = false
			s.abortErr = engines.ErrShellTerminated
		})
	} else {
		// otherwise wait for the result, and resolve when shell terminates
		go s.waitForResult()
	}

	return s, nil
}

func (s *execShell) StdinPipe() io.WriteCloser {
	return s.stdin
}

func (s *execShell) StdoutPipe() io.ReadCloser {
	return s.stdout
}

func (s *execShell) StderrPipe() io.ReadCloser {
	return s.stderr
}

func (s *execShell) SetSize(columns, rows uint16) error {
	if s.pty == nil {
		return nil
	}
	return s.pty.SetSize(columns, rows)
}

func (s *execShell) waitForResult() {
	err := s.cmd.Wait()
	s.resolve.Do(func() {
		s.stdin.Close()
		s.stdout.Close()
		s.stderr.Close()

		s.result = err != nil
		s.abortErr = engines.ErrShellTerminated
	})
}

func (s *execShell) Abort() error {
	s.resolve.Do(func() {
		// Kill process if one was started
		if s.cmd.Process != nil {
			s.cmd.Process.Kill()
		}

		s.stdin.Close()
		s.stdout.Close()
		s.stderr.Close()

		s.result = false
		s.resultErr = engines.ErrShellAborted
	})
	s.resolve.Wait()
	return s.abortErr
}

func (s *execShell) Wait() (bool, error) {
	s.resolve.Wait()
	return s.result, s.resultErr
}

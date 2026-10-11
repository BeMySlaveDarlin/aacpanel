package executor

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type ptyPair struct {
	master *os.File
	slave  *os.File
}

func openPTY(cols, rows uint16) (*ptyPair, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("/dev/ptmx did not open: %w", err)
	}

	if err := ioctl(master, func(fd int) error { return unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0) }); err != nil {
		master.Close()
		return nil, fmt.Errorf("the pty pair was not unlocked: %w", err)
	}
	var num int
	if err := ioctl(master, func(fd int) (err error) {
		num, err = unix.IoctlGetInt(fd, unix.TIOCGPTN)
		return err
	}); err != nil {
		master.Close()
		return nil, fmt.Errorf("the pty pair number cannot be read: %w", err)
	}

	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", num), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("/dev/pts/%d did not open: %w", num, err)
	}

	p := &ptyPair{master: master, slave: slave}
	if err := p.resize(cols, rows); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func (p *ptyPair) resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return fmt.Errorf("window size %dx%d — a terminal does not take that", cols, rows)
	}
	ws := unix.Winsize{Row: rows, Col: cols}
	if err := ioctl(p.master, func(fd int) error { return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &ws) }); err != nil {
		return fmt.Errorf("window size was not set: %w", err)
	}
	return nil
}

// ioctl runs a call on the descriptor of the master side without taking it
// out of the runtime's poller, as Fd would: Fd puts the descriptor in blocking
// mode, and then Close cannot interrupt a Read standing in it. The descriptor
// stays open until that Read returns, the client on the other side is never
// hung up, and it stays attached until it is killed.
func ioctl(f *os.File, call func(fd int) error) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var callErr error
	if err := raw.Control(func(fd uintptr) { callErr = call(int(fd)) }); err != nil {
		return err
	}
	return callErr
}

func (p *ptyPair) attach(cmd *exec.Cmd) {
	cmd.Stdin = p.slave
	cmd.Stdout = p.slave
	cmd.Stderr = p.slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
}

func (p *ptyPair) closeSlave() {
	if p.slave != nil {
		p.slave.Close()
		p.slave = nil
	}
}

func (p *ptyPair) Close() error {
	p.closeSlave()
	if p.master != nil {
		err := p.master.Close()
		p.master = nil
		return err
	}
	return nil
}

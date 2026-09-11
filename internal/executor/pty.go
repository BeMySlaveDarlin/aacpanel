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

	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		return nil, fmt.Errorf("the pty pair was not unlocked: %w", err)
	}
	num, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
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
	if err := unix.IoctlSetWinsize(int(p.master.Fd()), unix.TIOCSWINSZ, &ws); err != nil {
		return fmt.Errorf("window size was not set: %w", err)
	}
	return nil
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

// Copyright 2011 Aaron Jacobs. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This file contains OS-specific constants and types that work on OS X (tested
// on version 10.6.8).
//
// Helpful documentation for some of these options:
//
//     http://www.unixwiz.net/techtips/termios-vmin-vtime.html
//     http://www.taltech.com/support/entry/serial_intro
//     http://www.cs.utah.edu/dept/old/texinfo/glibc-manual-0.02/library_16.html
//     http://permalink.gmane.org/gmane.linux.kernel/103713
//

package serial

import (
	"errors"
	"io"
)
import "math"
import "os"
import "syscall"
import "unsafe"

// termios types
type cc_t byte
type speed_t uint64
type tcflag_t uint64

// sys/termios.h
const (
	kCS5    = 0x00000000
	kCS6    = 0x00000100
	kCS7    = 0x00000200
	kCS8    = 0x00000300
	kCLOCAL = 0x00008000
	kCREAD  = 0x00000800
	kCSTOPB = 0x00000400
	kIGNPAR = 0x00000004
	kPARENB = 0x00001000
	kPARODD = 0x00002000

	kNCCS = 20

	kVMIN  = tcflag_t(16)
	kVTIME = tcflag_t(17)
)

// sys/ttycom.h
const (
	kTIOCGETA = 1078490131
	kTIOCSETA = 2152231956
)

// sys/termios.h
type termios struct {
	c_iflag  tcflag_t
	c_oflag  tcflag_t
	c_cflag  tcflag_t
	c_lflag  tcflag_t
	c_cc     [kNCCS]cc_t
	c_ispeed speed_t
	c_ospeed speed_t
}

// setTermios updates the termios struct associated with a serial port file
// descriptor. This sets appropriate options for how the OS interacts with the
// port.
func setTermios(fd int, src *termios) error {
	// Make the ioctl syscall that sets the termios struct.
	r1, _, errno :=
		syscall.Syscall(
			syscall.SYS_IOCTL,
			uintptr(fd),
			uintptr(kTIOCSETA),
			uintptr(unsafe.Pointer(src)))

	// Did the syscall return an error?
	if err := os.NewSyscallError("SYS_IOCTL", int(errno)); err != nil {
		return err
	}

	// Just in case, check the return value as well.
	if r1 != 0 {
		return errors.New("Unknown error from SYS_IOCTL.")
	}

	return nil
}

func round(f float64) float64 {
	return math.Floor(f + 0.5)
}

func convertOptions(options OpenOptions) (*termios, error) {
	var result termios

	// Ignore modem status lines. We don't want to receive SIGHUP when the serial
	// port is disconnected, for example.
	result.c_cflag |= kCLOCAL

	// Enable receiving data.
	//
	// NOTE(jacobsa): I don't know exactly what this flag is for. The man page
	// seems to imply that it shouldn't really exist.
	result.c_cflag |= kCREAD

	// Sanity check inter-character timeout and minimum read size options.
	vtime := uint(round(float64(options.InterCharacterTimeout)/100.0) * 100)
	vmin := options.MinimumReadSize

	if vmin == 0 && vtime < 100 {
		return nil, errors.New("Invalid values for InterCharacterTimeout and MinimumReadSize.")
	}

	if vtime > 25500 {
		return nil, errors.New("Invalid value for InterCharacterTimeout.")
	}

	// Set VMIN and VTIME. Make sure to convert to tenths of seconds for VTIME.
	result.c_cc[kVTIME] = cc_t(vtime / 100)
	result.c_cc[kVMIN] = cc_t(vmin)

	// Baud rate
	switch options.BaudRate {
	case 50:
	case 75:
	case 110:
	case 134:
	case 150:
	case 200:
	case 300:
	case 600:
	case 1200:
	case 1800:
	case 2400:
	case 4800:
	case 7200:
	case 9600:
	case 14400:
	case 19200:
	case 28800:
	case 38400:
	case 57600:
	case 76800:
	case 115200:
	case 230400:
	default:
		return nil, errors.New("Invalid setting for BaudRate.")
	}

	// On OS X, the termios.h constants for speeds just map to the values
	// themselves.
	result.c_ispeed = speed_t(options.BaudRate)
	result.c_ospeed = speed_t(options.BaudRate)

	// Data bits
	switch options.DataBits {
	case 5:
		result.c_cflag |= kCS5
	case 6:
		result.c_cflag |= kCS6
	case 7:
		result.c_cflag |= kCS7
	case 8:
		result.c_cflag |= kCS8
	default:
		return nil, errors.New("Invalid setting for DataBits.")
	}

	// Stop bits
	switch options.StopBits {
	case 1:
		// Nothing to do; CSTOPB is already cleared.
	case 2:
		result.c_cflag |= kCSTOPB
	default:
		return nil, errors.New("Invalid setting for StopBits.")
	}

	// Parity mode
	switch options.ParityMode {
	case PARITY_NONE:
		// Nothing to do; PARENB is already not set.
	case PARITY_ODD:
		// Enable parity generation and receiving at the hardware level using
		// PARENB, but continue to deliver all bytes to the user no matter what (by
		// not setting INPCK). Also turn on odd parity mode.
		result.c_cflag |= kPARENB
		result.c_cflag |= kPARODD
	case PARITY_EVEN:
		// Enable parity generation and receiving at the hardware level using
		// PARENB, but continue to deliver all bytes to the user no matter what (by
		// not setting INPCK). Leave out PARODD to use even mode.
		result.c_cflag |= kPARENB
	default:
		return nil, errors.New("Invalid setting for ParityMode.")
	}

	return &result, nil
}

func openInternal(options OpenOptions) (io.ReadWriteCloser, error) {
	// Open the serial port in non-blocking mode, since otherwise the OS will
	// wait for the CARRIER line to be asserted.
	file, err :=
		os.OpenFile(
			options.PortName,
			os.O_RDWR|os.O_NOCTTY|os.O_NONBLOCK,
			0600)

	if err != nil {
		return nil, err
	}

	// We want to do blocking I/O, so clear the non-blocking flag set above.
	r1, _, errno :=
		syscall.Syscall(
			syscall.SYS_FCNTL,
			uintptr(file.Fd()),
			uintptr(syscall.F_SETFL),
			uintptr(0))

	if err := os.NewSyscallError("SYS_IOCTL", int(errno)); err != nil {
		return nil, err
	}

	if r1 != 0 {
		return nil, errors.New("Unknown error from SYS_FCNTL.")
	}

	// Set appropriate options.
	terminalOptions, err := convertOptions(options)
	if err != nil {
		return nil, err
	}

	err = setTermios(file.Fd(), terminalOptions)
	if err != nil {
		return nil, err
	}

	// We're done.
	return file, nil
}

// Generic I2C driver.

package embd

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type i2cDriver struct {
	busMap     map[byte]*i2cBus
	busMapLock sync.Mutex
}

func newI2CDriver() I2CDriver {
	return &i2cDriver{
		busMap: make(map[byte]*i2cBus),
	}
}

func (i *i2cDriver) Bus(l byte) I2CBus {
	i.busMapLock.Lock()
	defer i.busMapLock.Unlock()

	var b *i2cBus

	if b = i.busMap[l]; b == nil {
		b = &i2cBus{l: l}
		i.busMap[l] = b
	}

	return b
}

func (i *i2cDriver) Close() error {
	for _, b := range i.busMap {
		b.Close()

		delete(i.busMap, b.l)
	}

	return nil
}

const (
	delay = 20

	slaveCmd = 0x0703 // Cmd to set slave address
	rdrwCmd  = 0x0707 // Cmd to read/write data together

	rd = 0x0001
)

type i2c_msg struct {
	addr  uint16
	flags uint16
	len   uint16
	buf   uintptr
}

type i2c_rdwr_ioctl_data struct {
	msgs uintptr
	nmsg uint32
}

type i2cBus struct {
	l    byte
	file *os.File
	addr byte
	mu   sync.Mutex
}

func newI2CBus(l byte) (*i2cBus, error) {
	b := &i2cBus{l: l}

	var err error
	if b.file, err = os.OpenFile(fmt.Sprintf("/dev/i2c-%v", b.l), os.O_RDWR, os.ModeExclusive); err != nil {
		return nil, err
	}

	return b, nil
}

func (b *i2cBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.file.Close()
}

func (b *i2cBus) setAddress(addr byte) error {
	if addr != b.addr {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), slaveCmd, uintptr(addr)); errno != 0 {
			return syscall.Errno(errno)
		}

		b.addr = addr
	}

	return nil
}

func (b *i2cBus) ReadByte(addr byte) (byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.setAddress(addr); err != nil {
		return 0, err
	}

	bytes := make([]byte, 1)
	n, _ := b.file.Read(bytes)

	if n != 1 {
		return 0, fmt.Errorf("i2c: Unexpected number (%v) of bytes read", n)
	}

	return bytes[0], nil
}

func (b *i2cBus) WriteByte(addr, value byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.setAddress(addr); err != nil {
		return err
	}

	n, err := b.file.Write([]byte{value})

	if n != 1 {
		err = fmt.Errorf("i2c: Unexpected number (%v) of bytes written in WriteByte", n)
	}

	return err
}

func (b *i2cBus) WriteBytes(addr byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.setAddress(addr); err != nil {
		return err
	}

	for i := range value {
		n, err := b.file.Write([]byte{value[i]})

		if n != 1 {
			return fmt.Errorf("i2c: Unexpected number (%v) of bytes written in WriteBytes", n)
		}
		if err != nil {
			return err
		}

		time.Sleep(delay * time.Millisecond)
	}

	return nil
}

func (b *i2cBus) ReadFromReg(addr, reg byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.setAddress(addr); err != nil {
		return err
	}

	hdrp := (*reflect.SliceHeader)(unsafe.Pointer(&value))

	var messages [2]i2c_msg
	messages[0].addr = uint16(addr)
	messages[0].flags = 0
	messages[0].len = 1
	messages[0].buf = uintptr(unsafe.Pointer(&reg))

	messages[1].addr = uint16(addr)
	messages[1].flags = rd
	messages[1].len = uint16(len(value))
	messages[1].buf = uintptr(unsafe.Pointer(hdrp.Data))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&messages))
	packets.nmsg = 2

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

func (b *i2cBus) ReadByteFromReg(addr, reg byte) (byte, error) {
	buf := make([]byte, 1)
	if err := b.ReadFromReg(addr, reg, buf); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func (b *i2cBus) ReadWordFromReg(addr, reg byte) (uint16, error) {
	buf := make([]byte, 2)
	if err := b.ReadFromReg(addr, reg, buf); err != nil {
		return 0, err
	}
	return uint16((uint16(buf[0]) << 8) | uint16(buf[1])), nil
}

func (b *i2cBus) WriteToReg(addr, reg byte, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.setAddress(addr); err != nil {
		return err
	}

	outbuf := append([]byte{reg}, value...)

	hdrp := (*reflect.SliceHeader)(unsafe.Pointer(&outbuf))

	var message i2c_msg
	message.addr = uint16(addr)
	message.flags = 0
	message.len = uint16(len(outbuf))
	message.buf = uintptr(unsafe.Pointer(&hdrp.Data))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&message))
	packets.nmsg = 1

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

func (b *i2cBus) WriteByteToReg(addr, reg, value byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.setAddress(addr); err != nil {
		return err
	}

	outbuf := [...]byte{
		reg,
		value,
	}

	var message i2c_msg
	message.addr = uint16(addr)
	message.flags = 0
	message.len = uint16(len(outbuf))
	message.buf = uintptr(unsafe.Pointer(&outbuf))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&message))
	packets.nmsg = 1

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

func (b *i2cBus) WriteWordToReg(addr, reg byte, value uint16) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.setAddress(addr); err != nil {
		return err
	}

	outbuf := [...]byte{
		reg,
		byte(value >> 8),
		byte(value),
	}

	var messages i2c_msg
	messages.addr = uint16(addr)
	messages.flags = 0
	messages.len = uint16(len(outbuf))
	messages.buf = uintptr(unsafe.Pointer(&outbuf))

	var packets i2c_rdwr_ioctl_data

	packets.msgs = uintptr(unsafe.Pointer(&messages))
	packets.nmsg = 1

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, b.file.Fd(), rdrwCmd, uintptr(unsafe.Pointer(&packets))); errno != 0 {
		return syscall.Errno(errno)
	}

	return nil
}

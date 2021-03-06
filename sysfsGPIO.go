/*
Copyright (c) 2018 Forrest Sibley <My^Name^Without^The^Surname@ieee.org>

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package sysfsGPIO

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// These are defines for the Epoll system. At the time that this code was written, poll() and select() were not
// implemented in golang, and epoll() is implemented but might not be fully implemented. syscall.EPOLLIN functions as
// expected, but syscall.EPOLLET does not. The following 1 << 31 shift came from the single epoll() go example
// that I was able to find; someone else apparently ran into similar problems. Upon further examination, the difference
// is in the sign: syscall.EPOLLET is -2147483648 while the EPOLLET below is the absolute value of it, e.g. there
// seems to be an issue with the signed math in the Go library.
//
// Someone else found this problem.
// https://github.com/golang/go/issues/5328
// The constant is apparently corrected elsewhere.
// https://godoc.org/golang.org/x/sys/unix

const (
	EPOLLET = unix.EPOLLET
	// EPOLLET = 1 << 31
	// Maximum number of epoll events. This parameter is fed to the kernel.
	MaxPollEvents = 32
	// This is set to an arbitrarily high value and should be more than enough for an RPi Zero.
	MaxIOPinCount = 128
)

// Epoll data struct. This struct should be created only once per process and should contain all of the information
// needed for the Epoll call.
var epollData struct {
	// Epoll file descriptor
	fd int
	// Single Epoll event and an array corresponding to all the events that the OS will describe after returning
	event  syscall.EpollEvent
	events [MaxPollEvents]syscall.EpollEvent
}

// A single RPi GPIO pin
type IOPin struct {
	// The GPIO number (important)
	GPIONum int
	// Input or output
	// Valid values are strings "in" or "out"
	Direction string
	// Edge to trigger on
	// Valid values are "rising" or "falling"
	TriggerEdge string
	// Sysfs file
	SysfsFile *os.File
	// Enabled flag for internal use. This inhibits read or write operations to pins.
	Enabled bool
}

// A map of file descriptors to *IOPin. This is needed to back-reference the file descriptor returned by the kernel to
// an IOPin struct.
var fileDescriptorMap map[int32]*IOPin

// Initialize a GPIO pin
func InitPin(gpioNum int, direction string) (*IOPin, error) {
	pin := IOPin{
		GPIONum:     gpioNum,
		Direction:   direction,
		TriggerEdge: "rising",
		Enabled:     true,
	}
	// Check to see whether the pin has already been exported
	exportedCheckPath := "/sys/class/gpio/gpio" + strconv.Itoa(pin.GPIONum)
	_, err := os.Stat(exportedCheckPath)

	// If the file corresponding to the exported pin does not exist, create it
	if os.IsNotExist(err) {
		// Convert the pin number to something that can be written by the
		// ioutil file writer to sysfs format
		sysfsPinNumber := []byte(strconv.Itoa(pin.GPIONum))
		// Export the pin
		err := ioutil.WriteFile("/sys/class/gpio/export", sysfsPinNumber, os.ModeDevice|os.ModeCharDevice)
		if err != nil {
			return nil, err
		}
	}

	// Set the direction: "in" (input) or "out" (output)
	directionFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.GPIONum) + "/direction"
	sysfsPinDirection := []byte(pin.Direction)
	err = ioutil.WriteFile(directionFileName, sysfsPinDirection, os.ModeDevice|os.ModeCharDevice)
	if err != nil {
		return nil, err
	}

	// Set the interrupt edge if applicable: "rising" or "falling" or "both" or "none"
	// Note: there are no checks done here on the validity of the edge type. Whatever is in the struct by default
	// is what will be set here. This is fairly safe as the struct is private.
	if pin.Direction == "in" && len(pin.TriggerEdge) != 0 {
		edgeFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.GPIONum) + "/edge"
		sysfsPinEdge := []byte(pin.TriggerEdge)
		err = ioutil.WriteFile(edgeFileName, sysfsPinEdge, os.ModeDevice|os.ModeCharDevice)
		if err != nil {
			return nil, err
		}
	}

	// Open and leave open the device file for reading or writing digital data
	valueFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.GPIONum) + "/value"
	if pin.Direction == "out" {
		pin.SysfsFile, err = os.OpenFile(valueFileName, os.O_RDWR, 0660)
	} else {
		pin.SysfsFile, err = os.OpenFile(valueFileName, os.O_RDONLY, 0660)
	}
	if err != nil {
		return nil, err
	}

	// Create a mapping from file descriptor to *IOPin
	fileDescriptorMap[int32(pin.SysfsFile.Fd())] = &pin

	return &pin, nil
}

// Set a pin's interrupt trigger edge as rising, falling, both, or none
func (pin *IOPin) SetTriggerEdge(triggerEdge string) error {
	// Check for a valid input before writing to SysFS file
	if triggerEdge == "rising" || triggerEdge == "falling" || triggerEdge == "both" || triggerEdge == "none" {
		pin.TriggerEdge = triggerEdge
	} else {
		return errors.New("Error: Invalid trigger edge specified")
	}

	// Write to SysFS file
	edgeFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.GPIONum) + "/edge"
	sysfsPinEdge := []byte(pin.TriggerEdge)
	err := ioutil.WriteFile(edgeFileName, sysfsPinEdge, os.ModeDevice|os.ModeCharDevice)
	if err != nil {
		return err
	}

	return nil
}

// Release the GPIO pin and close sysfs files
func (pin *IOPin) ReleasePin() error {
	// Set the pin to be an input. This operation is likely overkill on some systems and is put here as added
	// protection that the pin will not be in output state when it is un-exported in SysFS.
	pin.Direction = "in"
	pin.Enabled = false
	directionFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.GPIONum) + "/direction"
	sysfsPinDirection := []byte(pin.Direction)
	err := ioutil.WriteFile(directionFileName, sysfsPinDirection, os.ModeDevice|os.ModeCharDevice)
	if err != nil {
		return err
	}

	// Close the device file
	err = pin.SysfsFile.Close()
	if err != nil {
		return err
	}

	// Un-export the pin in Sysfs

	// Convert the pin number to something that can be written by ioutil
	// file writer to sysfs
	sysfsPinNumber := []byte(strconv.Itoa(pin.GPIONum))
	// Unxport the pin
	err = ioutil.WriteFile("/sys/class/gpio/unexport", sysfsPinNumber, os.ModeDevice|os.ModeCharDevice)
	if err != nil {
		return err
	}

	return nil
}

// Set an output GPIO pin high
func (pin *IOPin) SetHigh() error {
	if pin.Enabled {
		_, err := pin.SysfsFile.Write([]byte("1"))
		if err != nil {
			return err
		}
	}
	return nil
}

// Set an output GPIO pin low
func (pin *IOPin) SetLow() error {
	if pin.Enabled {
		_, err := pin.SysfsFile.Write([]byte("0"))
		if err != nil {
			return err
		}
	}
	return nil
}

// Read an input GPIO pin and return 0 for low or 1 for high
func (pin *IOPin) Read() (int, error) {
	if pin.Enabled {
		readBuffer := make([]byte, 1)
		// Must rewind for every read
		pin.SysfsFile.Seek(0, 0)
		_, err := pin.SysfsFile.Read(readBuffer)
		if err != nil {
			return -1, err
		}
		state := int(readBuffer[0] & 1)
		return state, nil
	} else {
		return -1, nil
	}
}

// Set up a GPIO pin to be both an input and an interrupt pin
func (pin *IOPin) AddPinInterrupt() error {
	fdGpio := pin.SysfsFile

	// Criteria: Input and edge-triggered
	epollData.event.Events = syscall.EPOLLIN | EPOLLET
	epollData.event.Fd = int32(fdGpio.Fd())
	err := syscall.EpollCtl(epollData.fd, syscall.EPOLL_CTL_ADD, int(fdGpio.Fd()), &epollData.event)

	if err != nil {
		return err
	}

	return nil
}

// TODO: Finish and test this function

// Remove the monitoring of a GPIO pin
func (pin *IOPin) DeletePinInterrupt() error {
	fdGpio := pin.SysfsFile

	fmt.Println("Before:", epollData.fd, int(fdGpio.Fd()), &epollData.event)

	epollData.event.Fd = int32(fdGpio.Fd())
	err := syscall.EpollCtl(epollData.fd, syscall.EPOLL_CTL_DEL, int(fdGpio.Fd()), &epollData.event)

	fmt.Println("After:", epollData.fd, int(fdGpio.Fd()), &epollData.event)

	if err != nil {
		return err
	}

	return nil
}

type InterruptData struct {
	IOPin       *IOPin
	Edge        string
	StateString string
	// StateInt is unimplemented. I may consider taking this out.
	StateInt    int
}

// Global variable used in init()
var intStream <-chan InterruptData

func GetInterruptStream() <-chan InterruptData {
	return intStream
}

// Interrupt service routine by loose definition
func isr() (interruptStream chan InterruptData) {

	// Bidirectional channel returned by this function. This will be converted to a read-only channel in init().
	interruptStream = make(chan InterruptData, MaxPollEvents)

	// Spin the EpollWait() call off into a separate goroutine. If something happens, feed it into the channel.
	go func() {
		for {
			// This call will block until the kernel has something ready
			numEvents, err := syscall.EpollWait(epollData.fd, epollData.events[:], -1)

			if err != nil {
				fmt.Println("epoll_wait error ", err)
			}

			fmt.Println("numEvents: ", numEvents)
			for ev := 0; ev < numEvents; ev++ {
				ioPin := fileDescriptorMap[int32(epollData.events[ev].Fd)]
				// Note: There is a possibility that this value can be wrong if the pin has been
				// modified by another process. It is much faster to use the edge value already in
				// this program's memory than to go back to SysFS and poll another file.
				edge := ioPin.TriggerEdge
				var stateString string
				var stateInt int

				if edge == "rising" {
					stateString = "high"
					stateInt = 1
				} else if edge == "falling" {
					stateString = "low"
					stateInt = 0
				} else if edge == "both" {
					stateInt, _ = ioPin.Read()
					if stateInt == 0 {
						stateString = "low"
					} else if stateInt == 1 {
						stateString = "high"
					}
				} else if edge == "none" {
				}

				// Do not allow the channel to overflow
				if len(interruptStream) != cap(interruptStream) {
					interruptStream <- InterruptData{ioPin, edge, stateString, stateInt}
				}
			}
		}
	}()

	return interruptStream
}

func init() {
	// Create the map for referencing file descriptors to IOPins
	fileDescriptorMap = make(map[int32]*IOPin, MaxIOPinCount)

	// Initialize the epollData file descriptor here. It should be only initialized once per process.
	var err error
	epollData.fd, err = syscall.EpollCreate1(0)

	if err != nil {
		fmt.Println("epoll_create1 error: ", err)
	}

	intStream = isr()

	// Handle SIGINT events
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			fmt.Println("Interrupt signal received:", sig)

			for _, pin := range fileDescriptorMap {
				//				fmt.Println(pin)
				pin.Enabled = false
				err := pin.ReleasePin()

				if err != nil {
					fmt.Println("Error releasing pin upon program exit:", err)
				}
			}

			fmt.Println("Pins have been released in SysFS.")

			os.Exit(1)
		}
	}()
}

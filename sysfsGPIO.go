package main

import (
	"fmt"
	//   "bufio"
	//   "io"
	"io/ioutil"
	"os"
	"strconv"
	//"sync"
	"syscall"
	"time"
)

// A single RPi GPIO pin
type ioPin struct {
	// The GPIO number (important)
	gpioNum int
	// The number of the pin on the header (unimportant and here for
	// convenience)
	headerNum int
	// Input or output
	// Valid values are strings "in" or "out"
	direction string
	// Edge to trigger on
	// Valid values are "rising" or "falling"
	triggerEdge string
	// Sysfs file
	sysfsFile *os.File
}

// Initialize a GPIO pin
func (pin *ioPin) init() {
	// Check to see whether the pin has already been exported
	exportedCheckPath := "/sys/class/gpio/gpio" + strconv.Itoa(pin.gpioNum)
	_, err := os.Stat(exportedCheckPath)

	// If the file corresponding to the exported pin does not exist, create it
	if os.IsNotExist(err) {
		// Convert the pin number to something that can be written by the
		// ioutil file writer to sysfs format
		sysfsPinNumber := []byte(strconv.Itoa(pin.gpioNum))
		// Export the pin
		err := ioutil.WriteFile("/sys/class/gpio/export", sysfsPinNumber, os.ModeDevice|os.ModeCharDevice)
		fileCheck(err, true)
	}

	// Set the direction: "in" (input) or "out" (output)
	directionFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.gpioNum) + "/direction"
	sysfsPinDirection := []byte(pin.direction)
	err = ioutil.WriteFile(directionFileName, sysfsPinDirection, os.ModeDevice|os.ModeCharDevice)
	fileCheck(err)

	// Set the interrupt edge if applicable: "rising" or "falling" or "none"
	if pin.direction == "in" && len(pin.triggerEdge) != 0 {
		edgeFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.gpioNum) + "/edge"
		sysfs_pin_edge := []byte(pin.triggerEdge)
		err = ioutil.WriteFile(edgeFileName, sysfs_pin_edge, os.ModeDevice|os.ModeCharDevice)
	}

	// Open and leave open the device file for reading or writing digital data
	value_file_name := "/sys/class/gpio/gpio" + strconv.Itoa(pin.gpioNum) + "/value"
	if pin.direction == "out" {
		pin.sysfsFile, err = os.OpenFile(value_file_name, os.O_RDWR, 0660)
	} else {
		pin.sysfsFile, err = os.OpenFile(value_file_name, os.O_RDONLY, 0660)
	}
	fileCheck(err)
}

// Release the GPIO pin and close sysfs files
func (pin *ioPin) release() {
	// Close the device file
	err := pin.sysfsFile.Close()
	fileCheck(err, true, "Error closing")

	// Un-export the pin in Sysfs

	// Convert the pin number to something that can be written by ioutil
	// file writer to sysfs
	sysfsPinNumber := []byte(strconv.Itoa(pin.gpioNum))
	// Unxport the pin
	err = ioutil.WriteFile("/sys/class/gpio/unexport", sysfsPinNumber, os.ModeDevice|os.ModeCharDevice)
	fileCheck(err, true, "Error unexporting")
}

// Set an output GPIO pin high
func (pin *ioPin) set_high() {
	_, err := pin.sysfsFile.Write([]byte("1"))
	fileCheck(err, true, "Error writing pin high")
}

// Set an output GPIO pin low
func (pin *ioPin) set_low() {
	_, err := pin.sysfsFile.Write([]byte("0"))
	fileCheck(err, true, "Error writing pin low")
}

// Read an input GPIO pin and return a byte slice
func (pin *ioPin) read() []byte {
	readBuffer := make([]byte, 16)
	// Must rewind for every read
	pin.sysfsFile.Seek(0, 0)
	_, err := pin.sysfsFile.Read(readBuffer)
	fileCheck(err, true, "Error reading pin")
	return readBuffer
}

// Keeping all the epoll data global as epoll should be created only once per process
var epollData struct {
	// Epoll file descriptor
	fd int
	// Single Epoll event and an array corresponding to all the events that the OS will describe after returning
	event  syscall.EpollEvent
	events [MaxPollEvents]syscall.EpollEvent
}

func (pin *ioPin) addInterruptPin() {

	fd_gpio := pin.sysfsFile

	// Criteria: Input and edge-triggered
	epollData.event.Events = syscall.EPOLLIN | EPOLLET
	epollData.event.Fd = int32(fd_gpio.Fd())
	err := syscall.EpollCtl(epollData.fd, syscall.EPOLL_CTL_ADD, int(fd_gpio.Fd()), &epollData.event)

	fmt.Println(epollData.fd, int(fd_gpio.Fd()), &epollData.event)

	if err != nil {
		fmt.Println("epollctl add error: ", err)
		os.Exit(1)
	}
}

// Interrupt service routine by loose definition
func isr(triggered chan int) {
	var err error
	epollData.fd, err = syscall.EpollCreate1(0)

	if err != nil {
		fmt.Println("epoll_create1 error: ", err)
		os.Exit(1)
	}

	// TODO: correct file closing, defer, etc.

	// Spin the EpollWait() call off into a separate goroutine. If something happens, feed it into the channel.
	go func() {
		for {
			// This call will block until the kernel has something ready
			numEvents, err := syscall.EpollWait(epollData.fd, epollData.events[:], -1)

			if err != nil {
				fmt.Println("epoll_wait error ", err)
				break

			}

			triggered <- 1

			fmt.Println("numEvents: ", numEvents)
			fmt.Println("events[0]: ", epollData.events[0])
			fmt.Println("events[0].Fd ", epollData.events[0].Fd)
		}
	}()
}

// Variadic function: one or two arguments.
// The first argument is an error, e.g. returned from a file operation
// The second argument is a boolean flag for whether the error should be treated as a warning
func fileCheck(args ...interface{}) {

	var err error
	var warn_only bool
	var tag string

	for i, val := range args {
		switch i {
		case 0:
			param, _ := val.(error)
			err = param
		case 1:
			param, _ := val.(bool)
			warn_only = param
		case 2:
			param, _ := val.(string)
			tag = param
		}
	}

	if err != nil {
		if warn_only {
			fmt.Println("Warning: problems were encountered opening the file as follows.")
			fmt.Println(tag)
			fmt.Println(err)
		} else {
			fmt.Println(tag)
			panic(err)
		}
	}
}

// These are defines for the Epoll system. At the time that this code was written, poll() and select() were not
// implemented in golang, and epoll() is implemented but might not be fully implemented. sysCall.EPOLLIN functions as
// expected, but sysCall.EPOLLET does not. The following 1 << 31 shift came from the single epoll() go example
// that I was able to find; someone else apparently ran into similar problems.
const (
	EPOLLET       = 1 << 31
	MaxPollEvents = 32
)

func main() {

	gpio2 := ioPin{
		gpioNum:   2,
		headerNum: 3,
		direction: "out"}

	gpio2.init()
	defer gpio2.release()

	gpio3 := ioPin{
		gpioNum:     3,
		headerNum:   5,
		direction:   "in",
		triggerEdge: "rising"}

	gpio3.init()
	defer gpio3.release()

	triggered3 := make(chan int)
	isr(triggered3)

	gpio3.addInterruptPin()

	for {
		fmt.Println(<-triggered3)
	}

	for {
		gpio2.set_high()
		time.Sleep(time.Millisecond * 1000)
		gpio2.set_low()
		time.Sleep(time.Millisecond * 1000)
		fmt.Println(gpio3.read())
	}

	/*
	   high := []byte("1")
	   low := []byte("0")

	   for {
	      ioutil.WriteFile("/sys/class/gpio/gpio2/value", high, 0600)
	      time.Sleep(time.Millisecond * 1)
	      ioutil.WriteFile("/sys/class/gpio/gpio2/value", low, 0600)
	      time.Sleep(time.Millisecond * 1)
	   }
	*/

}

package sysfsGPIO

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"syscall"
	"time"
)

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
}

// TODO: Create a function to set the trigger edge as rising or falling

// Initialize a GPIO pin
func InitPin(gpioNum int, direction string) (*IOPin, error){
	pin := IOPin {
		GPIONum: gpioNum,
		Direction: direction,
		TriggerEdge: "rising",
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

	// Set the interrupt edge if applicable: "rising" or "falling" or "none"
	if pin.Direction == "in" && len(pin.TriggerEdge) != 0 {
		edgeFileName := "/sys/class/gpio/gpio" + strconv.Itoa(pin.GPIONum) + "/edge"
		sysfs_pin_edge := []byte(pin.TriggerEdge)
		err = ioutil.WriteFile(edgeFileName, sysfs_pin_edge, os.ModeDevice|os.ModeCharDevice)
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

	return &pin, nil
}

// Release the GPIO pin and close sysfs files
func (pin *IOPin) ReleasePin() error {
	// Close the device file
	err := pin.SysfsFile.Close()
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
	_, err := pin.SysfsFile.Write([]byte("1"))
	if err != nil {
		return err
	}
	return nil
}

// Set an output GPIO pin low
func (pin *IOPin) SetLow() error {
	_, err := pin.SysfsFile.Write([]byte("0"))
	if err != nil {
		return err
	}
	return nil
}

// Read an input GPIO pin and return a byte slice
func (pin *IOPin) Read() ([]byte, error) {
	readBuffer := make([]byte, 16)
	// Must rewind for every read
	pin.SysfsFile.Seek(0, 0)
	_, err := pin.SysfsFile.Read(readBuffer)
	if err != nil {
		return nil, err
	}
	return readBuffer, nil
}

// Keeping all the epoll data global as epoll should be created only once per process
var epollData struct {
	// Epoll file descriptor
	fd int
	// Single Epoll event and an array corresponding to all the events that the OS will describe after returning
	event  syscall.EpollEvent
	events [MaxPollEvents]syscall.EpollEvent
}

func (pin *IOPin) AddInterruptPin() error {

	fd_gpio := pin.SysfsFile

	// Criteria: Input and edge-triggered
	epollData.event.Events = syscall.EPOLLIN | EPOLLET
	epollData.event.Fd = int32(fd_gpio.Fd())
	err := syscall.EpollCtl(epollData.fd, syscall.EPOLL_CTL_ADD, int(fd_gpio.Fd()), &epollData.event)

	fmt.Println(epollData.fd, int(fd_gpio.Fd()), &epollData.event)

	if err != nil {
		return err
	}

	return nil
}

// Interrupt service routine by loose definition
func (*IOPin) ISR(triggered chan int) {
	var err error
	epollData.fd, err = syscall.EpollCreate1(0)

	if err != nil {
		fmt.Println("epoll_create1 error: ", err)
	}

	// TODO: correct file closing, defer, etc.

	// Spin the EpollWait() call off into a separate goroutine. If something happens, feed it into the channel.
	go func() {
		for {
			// This call will block until the kernel has something ready
			numEvents, err := syscall.EpollWait(epollData.fd, epollData.events[:], -1)

			if err != nil {
				fmt.Println("epoll_wait error ", err)
			}

			triggered <- 1

			fmt.Println("numEvents: ", numEvents)
			fmt.Println("events[0]: ", epollData.events[0])
			fmt.Println("events[0].Fd ", epollData.events[0].Fd)
		}
	}()
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
	gpio2, _ := InitPin(2, "out")
	defer gpio2.ReleasePin()

	gpio3, _ := InitPin(3, "in")
	defer gpio3.ReleasePin()

	triggered3 := make(chan int)
	gpio3.ISR(triggered3)

	gpio3.AddInterruptPin()

	for {
		fmt.Println(<-triggered3)
	}

	for {
		gpio2.SetHigh()
		time.Sleep(time.Millisecond * 1000)
		gpio2.SetLow()
		time.Sleep(time.Millisecond * 1000)
		fmt.Println(gpio3.Read())
	}

}

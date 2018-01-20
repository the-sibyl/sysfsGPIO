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

package main

import (
	"fmt"
	"time"
	"github.com/the-sibyl/sysfsGPIO"
)

func main() {
	gpio2, _ := sysfsGPIO.InitPin(2, "in")
	defer gpio2.ReleasePin()

	gpio3, _ := sysfsGPIO.InitPin(26, "in")
	gpio3.SetTriggerEdge("both")
	defer gpio3.ReleasePin()

	gpio2.AddPinInterrupt()

	gpio3.AddPinInterrupt()
	interruptStream := sysfsGPIO.ISR()
	go func() {
		for {
			fmt.Println(<-interruptStream)
		}
	} ()

	fmt.Println("Hi.................")

/*
	for {
		time.Sleep(time.Millisecond * 300)
		blarf, _ := gpio3.Read()
		fmt.Println("3 read():", blarf)
		yarf, _ := gpio2.Read()
		fmt.Println("2 read():", yarf)
	}
*/

	for {
//		gpio2.SetHigh()
		time.Sleep(time.Millisecond * 1000)
//		gpio2.SetLow()
		time.Sleep(time.Millisecond * 1000)
//		fmt.Println(gpio3.Read())
	}

}

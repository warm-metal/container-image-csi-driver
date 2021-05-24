package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	target := os.Getenv("TARGET")
	if target == "" {
		panic("env TARGET is missing")
	}

	file := filepath.Join(target, "hostname")

	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		panic("env HOSTNAME is missing")
	}

	fmt.Println("If got a SIGUSR1, ${HOSTNAME} will be written to file ${TARGET}/hostname.")
	fmt.Println("If got a SIGUSR2, the hostname will be read from file ${TARGET}/hostname and compared to ${HOSTNAME} then exit.")

	ch := make(chan os.Signal)
	defer close(ch)
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGUSR2)
	defer signal.Stop(ch)

	for {
		select {
		case sig, ok := <-ch:
			if !ok {
				panic("signal channel is closed")
			}

			switch sig {
			case syscall.SIGUSR1:
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					panic(fmt.Sprintf("file %q should not exist", file))
				}

				fmt.Println("write hostname to file ", file)
				if err := ioutil.WriteFile(file, []byte(hostname), 0644); err != nil {
					panic(fmt.Sprintf("unable to write to file %q: %s", file, err))
				}
			case syscall.SIGUSR2:
				fmt.Println("read hostname from file ", file)
				data, err := ioutil.ReadFile(file)
				if err != nil {
					panic(fmt.Sprintf("unable to read file %q: %s", file, err))
				}

				if string(data) != hostname {
					panic(fmt.Sprintf("hostname not match, %q vs %q", hostname, string(data)))
				}
				return
			}
		}
	}
}

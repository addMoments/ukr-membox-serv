package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func Write_pid() {
	pid := os.Getpid()
	wd_dir, err := os.Getwd()

	if err != nil {
		fmt.Println("cwd err: pid is not witten, killing server")
		os.Exit(1)
	}

	serv_pid, err := os.OpenFile(filepath.Join(wd_dir, "shell", "serv_pid"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)

	if err != nil {
		fmt.Println("serv_pid file o err: pid is not witten, killing server")
		os.Exit(1)
	}

	defer serv_pid.Close()

	_, err = fmt.Fprintf(serv_pid, "%d\n", pid)

	if err != nil {
		fmt.Println("serv_pid file w err: pid is not witten, killing server")
		os.Exit(1)
	}

	fmt.Println("pid written", strconv.Itoa(pid))
}

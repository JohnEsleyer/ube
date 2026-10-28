package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ube run <file.ube> [program args...]")
	}
	start := time.Now()
	ast, report, err := loadAndCheck(args[0])
	if err != nil {
		return err
	}
	binary := filepath.Join(os.TempDir(), fmt.Sprintf("ube-run-%d", time.Now().UnixNano()))
	cPath := binary + ".c"
	defer os.Remove(cPath)
	defer os.Remove(binary)
	if err = compileNative(ast, cPath, binary, false, false, false); err != nil {
		return err
	}
	fmt.Printf("%s %s (%s)\n", paint("✓ Compiled", colorPurple), filepath.Base(args[0]), paint(time.Since(start).Round(time.Millisecond).String(), colorDim))
	printPurity(report)
	cmd := exec.Command(binary, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return exitCodeError{e.ExitCode()}
		}
		return err
	}
	return nil
}

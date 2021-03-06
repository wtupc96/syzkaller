// Copyright 2017 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

//go:generate bash -c "echo -en '// AUTOGENERATED FILE\n\n' > generated.go"
//go:generate bash -c "echo -en 'package kernel\n\n' >> generated.go"
//go:generate bash -c "echo -en 'const createImageScript = `#!/bin/bash\n' >> generated.go"
//go:generate bash -c "cat ../../tools/create-gce-image.sh | grep -v '#' >> generated.go"
//go:generate bash -c "echo -en '`\n\n' >> generated.go"

// Package kernel contains helper functions for working with Linux kernel
// (building kernel/image).
package kernel

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/syzkaller/pkg/osutil"
)

func Build(dir, compiler, config string) error {
	if err := osutil.CopyFile(config, filepath.Join(dir, ".config")); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}
	return build(dir, compiler)
}

// TODO(dvyukov): this is only for syz-gce, remove when syz-gce is deleted.
func BuildWithPartConfig(dir, compiler, config string) error {
	const timeout = 10 * time.Minute // default timeout for command invocations
	os.Remove(filepath.Join(dir, ".config"))
	configFile := filepath.Join(dir, "syz.config")
	if err := osutil.WriteFile(configFile, []byte(config)); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}
	defer os.Remove(configFile)
	if _, err := osutil.RunCmd(timeout, dir, "make", "defconfig"); err != nil {
		return err
	}
	if _, err := osutil.RunCmd(timeout, dir, "make", "kvmconfig"); err != nil {
		return err
	}
	if _, err := osutil.RunCmd(timeout, dir, "scripts/kconfig/merge_config.sh", "-n", ".config", configFile); err != nil {
		return err
	}
	return build(dir, compiler)
}

func build(dir, compiler string) error {
	const timeout = 10 * time.Minute // default timeout for command invocations
	if _, err := osutil.RunCmd(timeout, dir, "make", "olddefconfig"); err != nil {
		return err
	}
	// We build only bzImage as we currently don't use modules.
	// Build of a large kernel can take a while on a 1 CPU VM.
	cpu := strconv.Itoa(runtime.NumCPU())
	if _, err := osutil.RunCmd(3*time.Hour, dir, "make", "bzImage", "-j", cpu, "CC="+compiler); err != nil {
		return err
	}
	return nil
}

// CreateImage creates a disk image that is suitable for syzkaller.
// Kernel is taken from kernelDir, userspace system is taken from userspaceDir.
// Produces image and root ssh key in the specified files.
func CreateImage(kernelDir, userspaceDir, image, sshkey string) error {
	tempDir, err := ioutil.TempDir("", "syz-build")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	scriptFile := filepath.Join(tempDir, "create.sh")
	if err := osutil.WriteExecFile(scriptFile, []byte(createImageScript)); err != nil {
		return fmt.Errorf("failed to write script file: %v", err)
	}
	bzImage := filepath.Join(kernelDir, filepath.FromSlash("arch/x86/boot/bzImage"))
	if _, err := osutil.RunCmd(time.Hour, tempDir, scriptFile, userspaceDir, bzImage); err != nil {
		return fmt.Errorf("image build failed: %v", err)
	}
	if err := osutil.CopyFile(filepath.Join(tempDir, "disk.raw"), image); err != nil {
		return err
	}
	if err := osutil.CopyFile(filepath.Join(tempDir, "key"), sshkey); err != nil {
		return err
	}
	if err := os.Chmod(sshkey, 0600); err != nil {
		return err
	}
	return nil
}

func CompilerIdentity(compiler string) (string, error) {
	output, err := osutil.RunCmd(time.Minute, "", compiler, "--version")
	if err != nil {
		return "", err
	}
	if len(output) == 0 {
		return "", fmt.Errorf("no output from compiler --version")
	}
	return strings.Split(string(output), "\n")[0], nil
}

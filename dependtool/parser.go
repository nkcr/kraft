// Copyright 2019 The UNICORE Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file
//
// Author: Gaulthier Gain <gaulthier.gain@uliege.be>

package dependtool

import (
	"fmt"
	"regexp"
	"strings"

	u "tools/common"
)

type RecursiveData struct {
	data, glMap, printDep map[string][]string
	cmd, line             string
	listStr               []string
	level                 int
}

// --------------------------------Static Output--------------------------------

// parseReadELF parses the output of the 'readelf' command.
//
func parseReadELF(output string, data *u.StaticData) {
	types := map[string]bool{"FUNC": true, "FILE": true, "OBJECT": true}

	// Check the output of 'readelf' command
	for _, line := range strings.Split(output, "\n") {
		words := strings.Fields(line)

		if len(words) > 8 && types[words[3]] {
			symbol := strings.Split(words[7], "@")
			data.Symbols[symbol[0]] = symbol[1]
		}
	}
}

// parseNM parses the output of the 'nm' command.
//
func parseNM(output string, data *u.StaticData) {
	// Get the list of system calls
	systemCalls := initSystemCalls()

	// Check the output of 'nm' command
	var re = regexp.MustCompile(`(?m)([U|T|B|D]\s)(.*)\s*`)
	for _, match := range re.FindAllStringSubmatch(output, -1) {
		// Add to system calls map if symbol is a system call
		if _, isSyscall := systemCalls[match[2]]; isSyscall {
			data.SystemCalls[match[2]] = ""
		} else {
			data.Symbols[match[2]] = ""
		}
	}
}

// parsePackagesName parses the output of the 'apt-cache pkgnames' command.
//
// It returns a string which represents the name of application used by the
// package manager (apt, ...).
func parsePackagesName(output string) string {

	var i = 1
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if len(line) > 0 {
			fmt.Printf("%d) %s\n", i, line)
			i++
		}
	}

	var input int
	for true {
		fmt.Print("Please enter your choice (0 to exit): ")
		if _, err := fmt.Scanf("%d", &input); err != nil {
			u.PrintWarning("Choice must be numeric! Try again")
		} else {
			if input == 0 {
				u.PrintWarning("Abort dependencies analysis from apt-cache")
				return ""
			} else if (input >= 0) && (input <= i) {
				return lines[input-1]
			} else {
				u.PrintWarning("Invalid input! Try again")
			}
		}
	}
	return ""
}

const LEVEL = 5

// parseDependencies parses the output of the 'apt-cache depends' command.
//
// It returns a slice of strings which represents all the dependencies of
// a particular package.
func parseDependencies(output string, data, dependenciesMap,
	printDep map[string][]string, fullDeps bool, level int) []string {
	listDep := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		if len(line) > 0 && !strings.Contains(line, "<") {

			if _, in := printDep[line]; !in {
				fmt.Println(line)
				printDep[line] = nil
			}

			if fullDeps && level < LEVEL {
				rd := RecursiveData{
					data:     data,
					glMap:    dependenciesMap,
					printDep: printDep,
					cmd: "apt-cache depends " + line +
						" | awk '/Depends/ { print $2 }'",
					line:    line,
					listStr: listDep,
					level:   level,
				}
				listDep = append(listDep, line)
				parseRecursive(rd)
			}
		} else {
			data[line] = nil
		}
	}
	return listDep
}

// parseLDD parses the output of the 'ldd' command.
//
// It returns a slice of strings which represents all the shared libs of
// a particular package.
func parseLDD(output string, data map[string][]string, lddMap map[string][]string,
	fullDeps bool) []string {

	listLdd := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		words := strings.Fields(line)

		if len(words) == 2 {

			lib, path := words[0], words[1]

			// Execute ldd only if fullDeps mode is set
			if fullDeps {
				rd := RecursiveData{
					data:     data,
					glMap:    lddMap,
					printDep: nil,
					cmd:      "ldd " + path + " | awk '/ => / { print $1,$3 }'",
					line:     lib,
					listStr:  listLdd,
					level:    -1,
				}
				listLdd = append(listLdd, lib)
				parseRecursive(rd)
			} else {
				data[lib] = nil
			}
		}
	}
	return listLdd
}

// parseRecursive is used by parseDependencies and parseLDD to factorize code.
//
func parseRecursive(rD RecursiveData) {

	if _, in := rD.glMap[rD.line]; in {
		// Use additional map to avoid executing again ldd
		rD.data[rD.line] = rD.glMap[rD.line]
	} else {

		var libsAcc []string
		out, err := u.ExecutePipeCommand(rD.cmd)
		if err != nil {
			u.PrintErr(err)
		}

		if rD.printDep == nil {
			libsAcc = parseLDD(out, rD.data, rD.glMap, true)
		} else {
			libsAcc = parseDependencies(out, rD.data, rD.glMap, rD.printDep,
				true, rD.level+1)
		}

		// Add return libsAcc to map
		rD.data[rD.line] = libsAcc
		rD.glMap[rD.line] = libsAcc
	}
}

// ------------------------------Dynamic Output --------------------------------

// parseTrace parses the output of the '(s)|(f)trace' command.
//
func parseTrace(output string, data map[string]string) {

	var re = regexp.MustCompile(`([a-zA-Z_0-9@/-]+?)\(.*`)
	for _, match := range re.FindAllStringSubmatch(output, -1) {
		// Add symbol to map
		data[match[1]] = ""
	}
}

// parseLsof parses the output of the 'lsof' command.
//
// It returns an error if any, otherwise it returns nil.
func parseLsof(output string, data *u.DynamicData, fullDeps bool) error {

	lddMap := make(map[string][]string)
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, ".so") {
			words := strings.Split(line, "/")
			data.SharedLibs[words[len(words)-1]] = nil
			if fullDeps {
				// Execute ldd only if fullDeps mode is set
				if out, err := u.ExecutePipeCommand("ldd " + line +
					" | awk '/ => / { print $1,$3 }'"); err != nil {
					return err
				} else {
					data.SharedLibs[words[len(words)-1]] =
						parseLDD(out, data.SharedLibs, lddMap, fullDeps)
				}
			}
		}
	}

	return nil
}
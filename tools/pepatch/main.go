package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var major, minor uint
	flag.UintVar(&major, "major", 6, "Mindest-Hauptversion (6 = Windows Vista…8.1)")
	flag.UintVar(&minor, "minor", 1, "Mindest-Nebenversion (6.1 = Windows 7 / Server 2008 R2)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "pepatch [-major N] [-minor N] datei.exe …  – setzt die Mindest-Windows-Version im PE-Kopf herab")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	rc := 0
	for _, path := range flag.Args() {
		if err := patchFile(path, uint16(major), uint16(minor)); err != nil {
			fmt.Fprintf(os.Stderr, "pepatch: %s: %v\n", path, err)
			rc = 1
		}
	}
	os.Exit(rc)
}

func patchFile(path string, major, minor uint16) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	oldMaj, oldMin, _ := minVersion(data)
	changed, err := patchMinVersion(data, major, minor)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Printf("pepatch: %s: Mindestversion bereits %d.%d\n", path, major, minor)
		return nil
	}
	if err := os.WriteFile(path, data, st.Mode().Perm()); err != nil {
		return err
	}
	fmt.Printf("pepatch: %s: Mindest-Windows-Version %d.%d → %d.%d\n", path, oldMaj, oldMin, major, minor)
	return nil
}

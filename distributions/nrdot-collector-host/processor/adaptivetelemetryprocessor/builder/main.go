package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: builder <distribution-name>")
	}

	distributionName := os.Args[1]
	log.Printf("Building distribution: %s", distributionName)

	// Run the OTel Collector Builder (ocb) to generate the distribution under _build
	cmd := exec.Command("ocb", "--config", "manifest.yaml", "--skip-compilation=false")
	cmd.Dir = filepath.Join("distributions", distributionName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Println("Running OpenTelemetry Collector Builder (ocb)...")
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to run builder: %v", err)
	}

	// Compile the generated distribution (components + main) in _build
	buildCmd := exec.Command("go", "build", "-o", distributionName)
	buildCmd.Dir = filepath.Join("distributions", distributionName, "_build")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	log.Println("Building the collector binary...")
	if err := buildCmd.Run(); err != nil {
		log.Fatalf("Failed to build the collector: %v", err)
	}

	log.Printf("Successfully built %s", distributionName)
}

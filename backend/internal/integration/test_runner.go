//go:build integration

// Package integration provides utilities for running integration tests
//
package integration

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// DockerComposeRunner manages docker compose services for integration tests
type DockerComposeRunner struct {
	ComposeFile string
	ProjectName string
}

// NewDockerComposeRunner creates a new docker compose runner
func NewDockerComposeRunner(composeFile string) *DockerComposeRunner {
	return &DockerComposeRunner{
		ComposeFile: composeFile,
		ProjectName: "netrunner-integration",
	}
}

// Start starts the docker compose services
func (r *DockerComposeRunner) Start() error {
	cmd := exec.Command("docker", "compose", "-f", r.ComposeFile, "-p", r.ProjectName, "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Stop stops the docker compose services
func (r *DockerComposeRunner) Stop() error {
	cmd := exec.Command("docker", "compose", "-f", r.ComposeFile, "-p", r.ProjectName, "down", "-v")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// WaitForHealthy waits for all services to be healthy
func (r *DockerComposeRunner) WaitForHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "compose", "-f", r.ComposeFile, "-p", r.ProjectName, "ps", "--format", "json")
		output, err := cmd.Output()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		
		// Simple check: if we got output, services are running
		if len(output) > 0 {
			// Check if all services are healthy
			psCmd := exec.Command("docker", "compose", "-f", r.ComposeFile, "-p", r.ProjectName, "ps")
			psOutput, _ := psCmd.Output()
			if strings.Contains(string(psOutput), "healthy") || strings.Contains(string(psOutput), "running") {
				return nil
			}
		}
		
		time.Sleep(2 * time.Second)
	}
	
	return fmt.Errorf("timeout waiting for services to be healthy")
}

// Logs shows the logs for a specific service
func (r *DockerComposeRunner) Logs(service string) error {
	cmd := exec.Command("docker", "compose", "-f", r.ComposeFile, "-p", r.ProjectName, "logs", service)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IntegrationTestRunner manages the full integration test lifecycle
type IntegrationTestRunner struct {
	ComposeRunner *DockerComposeRunner
}

// NewIntegrationTestRunner creates a new integration test runner
func NewIntegrationTestRunner() *IntegrationTestRunner {
	return &IntegrationTestRunner{
		ComposeRunner: NewDockerComposeRunner("docker-compose.integration.yml"),
	}
}

// Setup sets up the integration test environment
func (r *IntegrationTestRunner) Setup() error {
	fmt.Println("[Integration] Starting docker compose services...")
	if err := r.ComposeRunner.Start(); err != nil {
		return fmt.Errorf("failed to start services: %w", err)
	}
	
	fmt.Println("[Integration] Waiting for services to be healthy...")
	if err := r.ComposeRunner.WaitForHealthy(2 * time.Minute); err != nil {
		return fmt.Errorf("services failed to become healthy: %w", err)
	}
	
	fmt.Println("[Integration] Services are ready")
	return nil
}

// Teardown cleans up the integration test environment
func (r *IntegrationTestRunner) Teardown() error {
	fmt.Println("[Integration] Stopping docker compose services...")
	return r.ComposeRunner.Stop()
}

// SkipIfNoDocker skips the test if Docker is not available
func SkipIfNoDocker(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available")
	}
	
	// Check if Docker daemon is running
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker daemon not running")
	}
}

// SkipIfNoDockerCompose skips the test if Docker Compose is not available
func SkipIfNoDockerCompose(t *testing.T) {
	SkipIfNoDocker(t)
	
	// Try docker compose (v2) first
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		// Fall back to docker-compose (v1)
		cmd = exec.Command("docker-compose", "version")
		if err := cmd.Run(); err != nil {
			t.Skip("Docker Compose not available")
		}
	}
}

// GetEnvOrDefault gets an environment variable or returns a default value
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// MustGetEnv gets a required environment variable, fails test if not set
func MustGetEnv(t *testing.T, key string) string {
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("Required environment variable %s not set", key)
	}
	return value
}

// PrintIntegrationTestBanner prints a banner to identify integration test runs
func PrintIntegrationTestBanner() {
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("NetRunner Integration Tests")
	fmt.Println("Testing with dockerized slskd and real Soulseek protocol")
	fmt.Println(strings.Repeat("=", 60))
}

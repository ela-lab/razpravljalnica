package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// startTestServer starts a server binary on an available port
func startTestServer(t *testing.T) (int, func()) {
	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Get the project root directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Find the project root by looking for go.mod
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("Could not find project root")
		}
		wd = parent
	}

	// Start server process
	serverBin := filepath.Join(wd, "bin", "razpravljalnica-server")
	cmd := exec.Command(serverBin, "-p", fmt.Sprintf("%d", port))
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Wait for server to be ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return port, func() {
		cmd.Process.Kill()
	}
}

// runCLI runs the CLI with the given arguments and returns stdout and stderr
func runCLI(t *testing.T, args ...string) (string, string) {
	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")
	cmd := exec.Command(cliBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Don't fail on error - we want to test the output
		t.Logf("CLI command failed with error (this may be expected): %v", err)
	}

	return stdout.String(), stderr.String()
}

func TestCLIRegisterCommand(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond) // Wait for server to be ready

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")
	cmd := exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "register", "--name", "testuser")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
	}

	output := stdout.String()
	if output == "" {
		t.Fatal("Expected output from register command, got empty string")
	}
	if !bytes.Contains([]byte(output), []byte("testuser")) {
		t.Fatalf("Expected 'testuser' in output, got: %s", output)
	}
	t.Logf("✓ CLI register command works: %s", output)
}

func TestCLICreateTopicCommand(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond)

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")
	cmd := exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "create-topic", "--title", "TestTopic")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
	}

	output := stdout.String()
	if output == "" {
		t.Fatal("Expected output from create-topic command, got empty string")
	}
	if !bytes.Contains([]byte(output), []byte("TestTopic")) {
		t.Fatalf("Expected 'TestTopic' in output, got: %s", output)
	}
	t.Logf("✓ CLI create-topic command works: %s", output)
}

func TestCLIListTopicsCommand(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond)

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")

	// Create a topic first
	exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "create-topic", "--title", "Topic1").Run()

	cmd := exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "list-topics")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
	}

	output := stdout.String()
	if output == "" {
		t.Fatal("Expected output from list-topics command, got empty string")
	}
	t.Logf("✓ CLI list-topics command works")
}

func TestCLIPostMessageCommand(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond)

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")

	// Register user
	exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "register", "--name", "author").Run()

	// Create topic
	exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "create-topic", "--title", "Discussion").Run()

	// Post message
	cmd := exec.Command(cliBin, "-p", fmt.Sprintf("%d", port),
		"post-message", "--userId", "1", "--topicId", "1", "--message", "Hello World")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
	}

	output := stdout.String()
	if !bytes.Contains([]byte(output), []byte("Hello World")) {
		t.Fatalf("Expected 'Hello World' in output, got: %s", output)
	}
	t.Logf("✓ CLI post-message command works")
}

func TestCLIGlobalFlagsParsing(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond)

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")

	// Test with global -p flag before subcommand
	cmd := exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "register", "--name", "globalflagstest")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
	}

	output := stdout.String()
	if output == "" {
		t.Fatalf("Failed to parse global flags correctly")
	}
	if !bytes.Contains([]byte(output), []byte("globalflagstest")) {
		t.Fatalf("Global flag parsing failed, got: %s", output)
	}
	t.Logf("✓ Global flags (-p) parsed correctly: %s", output)
}

func TestCLISubcommandFlagsParsing(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond)

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")

	// Test with subcommand flags
	cmd := exec.Command(cliBin, "-p", fmt.Sprintf("%d", port), "register", "--name", "subflagstest")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
	}

	output := stdout.String()
	if !bytes.Contains([]byte(output), []byte("subflagstest")) {
		t.Fatalf("Subcommand flag parsing failed, got: %s", output)
	}
	t.Logf("✓ Subcommand flags (--name) parsed correctly")
}

func TestCLIMultipleFlagsIntegration(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond)

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")

	// Test: register user, create topic, post message
	tests := []struct {
		name        string
		args        []string
		expectInOut string
	}{
		{
			name:        "register user with name",
			args:        []string{"-p", fmt.Sprintf("%d", port), "register", "--name", "user1"},
			expectInOut: "user1",
		},
		{
			name:        "create topic with title",
			args:        []string{"-p", fmt.Sprintf("%d", port), "create-topic", "--title", "topic1"},
			expectInOut: "topic1",
		},
		{
			name:        "post message with all fields",
			args:        []string{"-p", fmt.Sprintf("%d", port), "post-message", "--userId", "1", "--topicId", "1", "--message", "msg1"},
			expectInOut: "msg1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(cliBin, test.args...)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout

			if err := cmd.Run(); err != nil {
				t.Logf("Command failed: %v", err)
			}

			output := stdout.String()
			if !bytes.Contains([]byte(output), []byte(test.expectInOut)) {
				t.Fatalf("Expected '%s' in output, got: %s", test.expectInOut, output)
			}
			t.Logf("✓ %s: parsed correctly", test.name)
		})
	}
}

func TestCLIArgumentOverride(t *testing.T) {
	port, cleanup := startTestServer(t)
	defer cleanup()

	time.Sleep(500 * time.Millisecond)

	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")

	// Test that global flags can override defaults
	cmd := exec.Command(cliBin, "-s", "127.0.0.1", "-p", fmt.Sprintf("%d", port), "register", "--name", "testoverride")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		t.Logf("Command failed: %v", err)
	}

	output := stdout.String()
	if !bytes.Contains([]byte(output), []byte("testoverride")) {
		t.Fatalf("Global flag override failed, got: %s", output)
	}
	t.Logf("✓ Global flags (-s and -p) override defaults correctly")
}

func TestCLIHelpCommand(t *testing.T) {
	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			break
		}
		wd = filepath.Dir(wd)
	}
	cliBin := filepath.Join(wd, "bin", "razpravljalnica-cli")
	cmd := exec.Command(cliBin, "--help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		// Help command might not fail, check output
	}

	output := stdout.String()
	if !bytes.Contains([]byte(output), []byte("register")) || !bytes.Contains([]byte(output), []byte("create-topic")) {
		t.Fatalf("Help output doesn't show commands, got: %s", output)
	}
	t.Logf("✓ CLI help command shows available commands")
}

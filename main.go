package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Configuration constants
const (
	AppName    = "SynologyDrive"
	ProcessName = "SynologyDrive" // Usually matches AppName based on `ps` output, checked in python script as "cloud-drive" for kill, "SynologyDrive" for start. 
    // Python script uses "pkill -f cloud-drive" and starts "SynologyDrive".
    // "open -a 'Synology Drive Client'" is what user tested. 
)

// Default ignore directories
var defaultIgnoreDirs = []string{
	// Common
	".git", ".gitignore", ".idea", ".vscode", ".cache", "dist", "build", "out", "target", "logs", "log",
	// Python
	"venv", ".venv", "__pycache__", ".pytest_cache", ".mypy_cache", ".tox", "*.egg-info", ".eggs", "htmlcov", ".coverage",
	// Node.js
	"node_modules", ".next", ".nuxt", ".output",
	// Go / PHP
	"vendor",
	// Swift / Xcode
	".build", "DerivedData", ".swiftpm", "Pods",
	// Flutter / Dart
	".dart_tool", ".flutter-plugins", ".flutter-plugins-dependencies", ".pub-cache", ".pub",
	// Rust
	"target", // Duplicate but harmless
	// Java / Kotlin
	".gradle", ".mvn",
	// Ruby
	".bundle",
	// Others
	".sass-cache", ".eslintcache", ".DS_Store",
}

// targetFiles are the configuration files we want to monitor and update
var targetFiles = []string{
	"blacklist.filter",
	"filter-v4150",
}

func main() {
	// 1. Setup paths
	usr, err := user.Current()
	if err != nil {
		log.Fatalf("Error getting current user: %v", err)
	}
	
	// Default Synology Drive config path on macOS
	confDir := filepath.Join(usr.HomeDir, "Library/Application Support/SynologyDrive/SynologyDrive.app/Contents/Resources/conf")
	
	log.Printf("Starting Synology Drive Watcher for: %s", confDir)

	// 2. Initial check
	if _, err := os.Stat(confDir); os.IsNotExist(err) {
		log.Fatalf("Config directory not found: %s", confDir)
	}

	// 3. Setup cleanup on exit
	// (Go doesn't have a direct equivalent to `atexit` simply, but we can rely on OS signals if we handled them. 
	// For now, let's keep it simple as a daemon-like process).

    // Initial enforce
    if rulesMissing(confDir) {
        log.Println("Initial rules check failed. Starting enforcement cycle.")
        enforceCycle(confDir)
    }

	// 4. Setup File Watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Channel to debounce events
	var (
		timer *time.Timer
		mu    sync.Mutex
	)

	// Watch loop
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				
				// We only care about writes to specific files
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
					filename := filepath.Base(event.Name)
					isTarget := false
					for _, t := range targetFiles {
						if filename == t {
							isTarget = true
							break
						}
					}

					if isTarget {
						mu.Lock()
						if timer != nil {
							timer.Stop()
						}
						// Debounce for 5 seconds (User request)
						timer = time.AfterFunc(5*time.Second, func() {
							log.Printf("Detected change in %s, checking rules...", filename)
                            // We only trigger if rules are missing.
                            // If rules are present, we do nothing.
                            // The enforceCycle will write them.
							if rulesMissing(confDir) {
                                log.Println("Rules missing, initiating enforcement cycle...")
								enforceCycle(confDir)
							} else {
                                log.Println("Rules present, no action needed.")
                            }
						})
						mu.Unlock()
					}
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Watcher error:", err)
			}
		}
	}()

	err = watcher.Add(confDir)
	if err != nil {
		log.Fatal(err)
	}

	// Keep running
	select {}
}

// checkAndEnforce checks all target files and updates them if needed. 
// Returns true if ANY file was updated.
func checkAndEnforce(confDir string) bool {
	updatedAny := false
	for _, filename := range targetFiles {
		path := filepath.Join(confDir, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			log.Printf("File not found: %s, skipping", path)
			continue
		}

		if ensureFileHasRules(path) {
			updatedAny = true
		}
	}
	return updatedAny
}

// ensureFileHasRules reads the file, checks for strict Ignore rules, and updates if missing.
// Returns true if file was modified.
func ensureFileHasRules(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Error reading file %s: %v", path, err)
		return false
	}

	lines := strings.Split(string(content), "\n")
    
    // Structure to hold config while preserving order
    type KeyVal struct {
        Key   string
        Value string
    }
    type Section struct {
        Name string
        Keys []KeyVal
    }
    
    var sections []*Section
    var currentSec *Section
    
    // Helper to find or create section
    getSection := func(name string) *Section {
        for _, s := range sections {
            if s.Name == name {
                return s
            }
        }
        // Create new
        newSec := &Section{Name: name, Keys: []KeyVal{}}
        sections = append(sections, newSec)
        return newSec
    }
    
    // 1. Parse
    for _, line := range lines {
        trimmed := strings.TrimSpace(line)
        if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
            secName := trimmed[1 : len(trimmed)-1]
            currentSec = getSection(secName)
        } else if strings.Contains(trimmed, "=") && currentSec != nil {
            parts := strings.SplitN(trimmed, "=", 2)
            key := strings.TrimSpace(parts[0])
            val := strings.TrimSpace(parts[1])
            
            // Check if key already exists in this section (handle duplicates if any, though unlikely in INI)
            exists := false
            for i, kv := range currentSec.Keys {
                if kv.Key == key {
                    currentSec.Keys[i].Value = val // Overwrite last seen
                    exists = true
                    break
                }
            }
            if !exists {
                currentSec.Keys = append(currentSec.Keys, KeyVal{Key: key, Value: val})
            }
        }
    }
    
    // 2. Update logic
    wasUpdated := false
    targetSections := []string{"Directory", "Common"}
    
    for _, secName := range targetSections {
        // We only check Common if it already exists or if we strictly need to add it?
        // Python behavior: "Common" checked only if it exists. "Directory" created if missing.
        
        // Check if section exists
        var sec *Section
        for _, s := range sections {
            if s.Name == secName {
                sec = s
                break
            }
        }
        
        if secName == "Directory" && sec == nil {
             sec = getSection("Directory")
             wasUpdated = true
        }
        
        if sec != nil {
             // Find "black_name"
             var kv *KeyVal
             for i := range sec.Keys {
                 if sec.Keys[i].Key == "black_name" {
                     kv = &sec.Keys[i]
                     break
                 }
             }
             
             currentVal := ""
             if kv != nil {
                 currentVal = kv.Value
             }
             
             newVal := ensureBlackList(currentVal)
             
             if currentVal != newVal {
                 if kv != nil {
                     kv.Value = newVal
                 } else {
                     sec.Keys = append(sec.Keys, KeyVal{Key: "black_name", Value: newVal})
                 }
                 wasUpdated = true
             }
        }
    }
    
    if !wasUpdated {
        return false
    }
    
    // 3. Reconstruct content
    var sb strings.Builder
    for _, sec := range sections {
        sb.WriteString(fmt.Sprintf("[%s]\n", sec.Name))
        for _, kv := range sec.Keys {
            sb.WriteString(fmt.Sprintf("%s=%s\n", kv.Key, kv.Value))
        }
        sb.WriteString("\n")
    }
    
    // Backup before write
    backupFile(path)
    
    // Write using sudo tee if needed (or standard write if permission ok)
    // Try standard write first
    err = os.WriteFile(path, []byte(sb.String()), 0644)
    if err != nil {
        if os.IsPermission(err) {
            // Try sudo tee
            log.Println("Permission denied, trying sudo...")
            return writeWithSudo(path, sb.String())
        }
        log.Printf("Failed to write file %s: %v", path, err)
        return false
    }
    
	return true
}

func ensureBlackList(current string) string {
    // Current format: "dir1", "dir2", "dir3"
    // We need to check if all defaultIgnoreDirs are present.
    // If not, append them.

    
    missing := []string{}
    for _, d := range defaultIgnoreDirs {
        // Simple check: check for "name" or name logic
        // The python logic was: f'"{dir}"' in current or dir in current
        // We should replicate exact logic.
        
        quoted := fmt.Sprintf("\"%s\"", d)
        if !strings.Contains(current, quoted) && !strings.Contains(current, d) {
            missing = append(missing, d)
        }
    }
    
    if len(missing) == 0 {
        return current
    }
    
    // Append missing
    if len(current) > 0 && !strings.HasSuffix(current, ", ") {
         current += ", "
    }
    
    toAppend := []string{}
    for _, m := range missing {
        toAppend = append(toAppend, fmt.Sprintf("\"%s\"", m))
    }
    
    return current + strings.Join(toAppend, ", ")
}


func backupFile(src string) {
    // Backup to the same directory as the executable (User request)
    exePath, err := os.Executable()
    if err != nil {
        log.Printf("Failed to get executable path for backup: %v", err)
        return
    }
    backupDir := filepath.Dir(exePath)
    
    filename := filepath.Base(src)
    timestamp := time.Now().Format("20060102_150405")
    backupName := fmt.Sprintf("%s.%s.backup", filename, timestamp)
    dst := filepath.Join(backupDir, backupName)
    
    sourceFile, err := os.Open(src)
    if err != nil {
        return
    }
    defer sourceFile.Close()

    destinationFile, err := os.Create(dst)
    if err != nil {
         log.Printf("Failed to create backup file %s: %v", dst, err)
        return
    }
    defer destinationFile.Close()
    
    io.Copy(destinationFile, sourceFile)
    log.Printf("Backed up %s to %s", src, dst)
}

func writeWithSudo(path string, content string) bool {
    // echo 'content' | sudo tee path > /dev/null
    cmd := exec.Command("sudo", "tee", path)
    cmd.Stdin = strings.NewReader(content)
    cmd.Stdout = nil // discard output
    err := cmd.Run()
    if err != nil {
        log.Printf("Failed to write with sudo: %v", err)
        return false
    }
    return true
}

// Check if rules are missing WITHOUT modifying
func rulesMissing(confDir string) bool {
    for _, filename := range targetFiles {
        path := filepath.Join(confDir, filename)
        if _, err := os.Stat(path); os.IsNotExist(err) {
            continue
        }
        if fileNeedsRules(path) {
            return true
        }
    }
    return false
}

// Check single file for missing rules (read-only check)
func fileNeedsRules(path string) bool {
    content, err := os.ReadFile(path)
    if err != nil {
        return false
    }
    // Simple substring check for performance before full parse
    // If all default dirs are present, we might skip
    // But exact check is better.
    // For now, let's reuse ensureFileHasRules logic but without writing?
    // Actually, ensureFileHasRules does both check and write.
    // Let's modify ensureFileHasRules to return true if it *would* modify, 
    // or just use it as is but pass a "dry run" flag?
    // Simpler: Just rely on checkAndEnforce causing a write.
    // But we want to Write AFTER Quit.
    
    // So:
    // 1. Read file.
    // 2. Parse.
    // 3. Check `black_name`.
    // 4. Return true if different.
    
    // To avoid code duplication, let's just use `ensureFileHasRules` in a special mode or accept it's slightly duplicate.
    // Let's copy the read logic or refactor.
    // Refactoring `ensureFileHasRules` to take an action func?
    
    // Quick implementation of check:
    current := string(content)
    for _, d := range defaultIgnoreDirs {
        quoted := fmt.Sprintf("\"%s\"", d)
        if !strings.Contains(current, quoted) && !strings.Contains(current, d) {
            return true
        }
    }
    return false
}

func enforceCycle(confDir string) {
    log.Println("Stopping Synology Drive Client to enforce config...")
    
    // 1. Quit
    cmdQuit := exec.Command("osascript", "-e", "tell application \"Synology Drive Client\" to quit")
    err := cmdQuit.Run()
    if err != nil {
        log.Printf("Failed to quit Drive Client via AppleScript: %v (trying pkill)", err)
        exec.Command("pkill", "-f", "cloud-drive-daemon").Run()
        exec.Command("pkill", "-f", "cloud-drive-ui").Run()
    }
    
    // 2. Wait for process to exit
    // Target: "cloud-drive-daemon" (User Request)
    // Reduce timeout to 60s (User feedback: 15s too short, 60s is better)
    timeout := time.After(60 * time.Second)
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    processOpen := true
    log.Println("Waiting for cloud-drive-daemon to exit (timeout 60s)...")
    
    for processOpen {
        select {
        case <-timeout:
            log.Println("Timeout (60s). Force killing cloud-drive-daemon...")
            exec.Command("pkill", "-9", "-f", "cloud-drive-daemon").Run()
            exec.Command("pkill", "-9", "-f", "cloud-drive-ui").Run()
            processOpen = false
        case <-ticker.C:
            // Check process "cloud-drive-daemon"
            cmdCheck := exec.Command("pgrep", "-f", "cloud-drive-daemon")
            if err := cmdCheck.Run(); err != nil {
                log.Println("cloud-drive-daemon process gone.")
                processOpen = false
            }
        // Removed logTicker to reduce spam in short timeout
        }
    }
    
    time.Sleep(1 * time.Second)
    
    // 3. Enforce Config (Write)
    // Now that process is gone, we write the config.
    log.Println("Enforcing configuration rules...")
    if checkAndEnforce(confDir) {
        log.Println("Configuration updated.")
    } else {
        log.Println("Configuration already correct (or write failed).")
    }
    
    // 4. Start
    log.Println("Starting Synology Drive Client...")
    cmdOpen := exec.Command("open", "-a", "Synology Drive Client")
    if err := cmdOpen.Run(); err != nil {
        log.Printf("Failed to start Drive Client: %v", err)
    }
    
    // Wait for process to appear (User request to confirm start)
    log.Println("Waiting for cloud-drive-daemon to allow...")
    startTimeout := time.After(30 * time.Second)
    startTicker := time.NewTicker(1 * time.Second)
    defer startTicker.Stop()
    
    started := false
    for !started {
        select {
        case <-startTimeout:
             log.Println("Timeout waiting for Drive to start (it might be running but not detected). Resuming watch anyway.")
             started = true
        case <-startTicker.C:
            if err := exec.Command("pgrep", "-f", "cloud-drive-daemon").Run(); err == nil {
                log.Println("Synology Drive started successfully.")
                started = true
            }
        }
    }
    
    notify("Synology Drive 配置已更新", "忽略规则已修复，应用已重启。")
    log.Println("Resuming configuration monitoring...")
}

// ... (main function needs update to call enforceCycle instead of checkAndEnforce directly in the watcher) ...


func notify(title, message string) {
    // osascript -e 'display notification "message" with title "title"'
    script := fmt.Sprintf("display notification \"%s\" with title \"%s\"", message, title)
    exec.Command("osascript", "-e", script).Run()
}

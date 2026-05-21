package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

var defaultIgnoreDirs = []string{
	".git", ".gitignore", ".idea", ".vscode", ".cache", "dist", "build", "out", "target", "logs", "log",
	"venv", ".venv", "__pycache__", ".pytest_cache", ".mypy_cache", ".tox", "*.egg-info", ".eggs", "htmlcov", ".coverage",
	"node_modules", ".next", ".nuxt", ".output",
	"vendor",
	".build", "DerivedData", ".swiftpm", "Pods",
	".dart_tool", ".flutter-plugins", ".flutter-plugins-dependencies", ".pub-cache", ".pub",
	"target",
	".gradle", ".mvn",
	".bundle",
	".sass-cache", ".eslintcache", ".DS_Store",
}

var targetFiles = []string{
	"blacklist.filter",
	"filter-v4150",
}

func main() {
	usr, err := user.Current()
	if err != nil {
		log.Fatalf("Error getting current user: %v", err)
	}

	synologyRoot := filepath.Join(usr.HomeDir, "Library/Application Support/SynologyDrive")
	confDirs := discoverConfDirs(synologyRoot)
	if len(confDirs) == 0 {
		log.Fatalf("No Synology Drive config directories found under: %s", synologyRoot)
	}

	log.Printf("Starting Synology Drive Watcher for %d config directories", len(confDirs))
	for _, confDir := range confDirs {
		log.Printf("Config directory: %s", confDir)
	}

	if rulesMissing(confDirs) {
		log.Println("Initial rules check failed. Starting enforcement cycle.")
		enforceCycle(synologyRoot)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	var (
		timer   *time.Timer
		mu      sync.Mutex
		watched = map[string]bool{}
	)

	addWatchDirs := func(dirs []string) {
		mu.Lock()
		defer mu.Unlock()

		for _, dir := range dirs {
			if watched[dir] {
				continue
			}
			if err := watcher.Add(dir); err != nil {
				log.Printf("Failed to watch %s: %v", dir, err)
				continue
			}
			watched[dir] = true
			log.Printf("Watching: %s", dir)
		}
	}

	addWatchDirs(discoverWatchDirs(synologyRoot))

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}

				addWatchDirs(discoverWatchDirs(synologyRoot))
				filename := filepath.Base(event.Name)
				if !isTargetFile(filename) && event.Op&(fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}

				eventName := event.Name
				mu.Lock()
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(5*time.Second, func() {
					currentConfDirs := discoverConfDirs(synologyRoot)
					log.Printf("Detected change in %s, checking %d config directories...", eventName, len(currentConfDirs))
					if rulesMissing(currentConfDirs) {
						log.Println("Rules missing, initiating enforcement cycle...")
						enforceCycle(synologyRoot)
					} else {
						log.Println("Rules present, no action needed.")
					}
				})
				mu.Unlock()

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Watcher error:", err)
			}
		}
	}()

	select {}
}

func discoverWatchDirs(synologyRoot string) []string {
	dataDir := filepath.Join(synologyRoot, "data")
	sessionDir := filepath.Join(dataDir, "session")

	candidates := append(discoverConfDirs(synologyRoot), dataDir, sessionDir)
	sessionDirs, err := filepath.Glob(filepath.Join(sessionDir, "*"))
	if err != nil {
		log.Printf("Failed to discover session directories: %v", err)
	}
	candidates = append(candidates, sessionDirs...)

	return uniqueExistingDirs(candidates)
}

func discoverConfDirs(synologyRoot string) []string {
	dataDir := filepath.Join(synologyRoot, "data")
	candidates := []string{
		dataDir,
	}

	sessionDirs, err := filepath.Glob(filepath.Join(dataDir, "session", "*", "conf"))
	if err != nil {
		log.Printf("Failed to discover session config directories: %v", err)
	}
	candidates = append(candidates, sessionDirs...)

	var confDirs []string
	for _, dir := range uniqueExistingDirs(candidates) {
		if hasAnyTargetFile(dir) {
			confDirs = append(confDirs, dir)
		}
	}
	return confDirs
}

func uniqueExistingDirs(candidates []string) []string {
	seen := map[string]bool{}
	var dirs []string

	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[candidate] = true
		dirs = append(dirs, candidate)
	}

	sort.Strings(dirs)
	return dirs
}

func hasAnyTargetFile(dir string) bool {
	for _, filename := range targetFiles {
		if _, err := os.Stat(filepath.Join(dir, filename)); err == nil {
			return true
		}
	}
	return false
}

func isTargetFile(filename string) bool {
	for _, targetFile := range targetFiles {
		if filename == targetFile {
			return true
		}
	}
	return false
}

func checkAndEnforce(confDirs []string) bool {
	updatedAny := false
	for _, confDir := range confDirs {
		for _, filename := range targetFiles {
			path := filepath.Join(confDir, filename)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			}
			if ensureFileHasRules(path) {
				updatedAny = true
			}
		}
	}
	return updatedAny
}

func ensureFileHasRules(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Error reading file %s: %v", path, err)
		return false
	}

	newContent, wasUpdated := ensureContentHasRules(string(content))
	if !wasUpdated {
		return false
	}

	backupFile(path)

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		if os.IsPermission(err) {
			log.Println("Permission denied, trying sudo...")
			return writeWithSudo(path, newContent)
		}
		log.Printf("Failed to write file %s: %v", path, err)
		return false
	}

	return true
}

func ensureContentHasRules(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	updatedAny := false

	var updated bool
	lines, updated = ensureSectionHasRules(lines, "Directory", true)
	updatedAny = updatedAny || updated

	lines, updated = ensureSectionHasRules(lines, "File", false)
	updatedAny = updatedAny || updated

	lines, updated = ensureSectionHasRules(lines, "Common", false)
	updatedAny = updatedAny || updated

	return strings.Join(lines, "\n"), updatedAny
}

func ensureSectionHasRules(lines []string, sectionName string, createIfMissing bool) ([]string, bool) {
	start, end, exists := findSectionRange(lines, sectionName)
	if !exists {
		if !createIfMissing {
			return lines, false
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf("[%s]", sectionName), fmt.Sprintf("black_name = %s", ensureBlackList("")))
		return lines, true
	}

	for lineIndex := start + 1; lineIndex < end; lineIndex++ {
		line := lines[lineIndex]
		equalsIndex := strings.Index(line, "=")
		if equalsIndex < 0 {
			continue
		}
		key := strings.TrimSpace(line[:equalsIndex])
		if key != "black_name" {
			continue
		}

		currentValue := strings.TrimSpace(line[equalsIndex+1:])
		newValue := ensureBlackList(currentValue)
		if currentValue == newValue {
			return lines, false
		}

		lines[lineIndex] = strings.TrimRight(line[:equalsIndex], " \t") + " = " + newValue
		return lines, true
	}

	insertIndex := end
	for insertIndex > start+1 && strings.TrimSpace(lines[insertIndex-1]) == "" {
		insertIndex--
	}

	newLine := fmt.Sprintf("black_name = %s", ensureBlackList(""))
	lines = append(lines[:insertIndex], append([]string{newLine}, lines[insertIndex:]...)...)
	return lines, true
}

func findSectionRange(lines []string, sectionName string) (int, int, bool) {
	for lineIndex, line := range lines {
		currentSection, ok := parseSectionName(line)
		if !ok || currentSection != sectionName {
			continue
		}

		end := len(lines)
		for nextIndex := lineIndex + 1; nextIndex < len(lines); nextIndex++ {
			if _, ok := parseSectionName(lines[nextIndex]); ok {
				end = nextIndex
				break
			}
		}

		return lineIndex, end, true
	}
	return 0, 0, false
}

func parseSectionName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return "", false
	}
	return trimmed[1 : len(trimmed)-1], true
}

func ensureBlackList(current string) string {
	present := parseBlackListValues(current)
	var missing []string

	for _, dir := range defaultIgnoreDirs {
		if present[dir] {
			continue
		}
		present[dir] = true
		missing = append(missing, dir)
	}

	if len(missing) == 0 {
		return current
	}

	toAppend := make([]string, 0, len(missing))
	for _, dir := range missing {
		toAppend = append(toAppend, fmt.Sprintf("%q", dir))
	}

	base := strings.TrimRight(strings.TrimSpace(current), " \t,")
	if base == "" {
		return strings.Join(toAppend, ", ")
	}
	return base + ", " + strings.Join(toAppend, ", ")
}

func parseBlackListValues(value string) map[string]bool {
	values := map[string]bool{}
	for _, part := range splitCommaList(value) {
		item := strings.TrimSpace(part)
		item = strings.Trim(item, "\"")
		if item != "" {
			values[item] = true
		}
	}
	return values
}

func splitCommaList(value string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	escaped := false

	for _, char := range value {
		switch {
		case escaped:
			current.WriteRune(char)
			escaped = false
		case char == '\\':
			current.WriteRune(char)
			escaped = true
		case char == '"':
			current.WriteRune(char)
			inQuote = !inQuote
		case char == ',' && !inQuote:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(char)
		}
	}
	parts = append(parts, current.String())
	return parts
}

func backupFile(src string) {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path for backup: %v", err)
		return
	}
	backupDir := filepath.Dir(exePath)

	filename := filepath.Base(src)
	timestamp := time.Now().UnixNano()
	backupName := fmt.Sprintf("%s.%d.backup", filename, timestamp)
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

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		log.Printf("Failed to backup %s to %s: %v", src, dst, err)
		return
	}
	log.Printf("Backed up %s to %s", src, dst)
}

func writeWithSudo(path string, content string) bool {
	cmd := exec.Command("sudo", "-n", "tee", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = nil
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to write with sudo: %v", err)
		return false
	}
	return true
}

func rulesMissing(confDirs []string) bool {
	for _, confDir := range confDirs {
		for _, filename := range targetFiles {
			path := filepath.Join(confDir, filename)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			}
			if fileNeedsRules(path) {
				return true
			}
		}
	}
	return false
}

func fileNeedsRules(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	_, needsUpdate := ensureContentHasRules(string(content))
	return needsUpdate
}

func enforceCycle(synologyRoot string) {
	log.Println("Stopping Synology Drive Client to enforce config...")

	cmdQuit := exec.Command("osascript", "-e", "tell application \"Synology Drive Client\" to quit")
	err := cmdQuit.Run()
	if err != nil {
		log.Printf("Failed to quit Drive Client via AppleScript: %v (trying pkill)", err)
		exec.Command("pkill", "-f", "cloud-drive-daemon").Run()
		exec.Command("pkill", "-f", "cloud-drive-ui").Run()
	}

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
			cmdCheck := exec.Command("pgrep", "-f", "cloud-drive-daemon")
			if err := cmdCheck.Run(); err != nil {
				log.Println("cloud-drive-daemon process gone.")
				processOpen = false
			}
		}
	}

	time.Sleep(1 * time.Second)

	log.Println("Enforcing configuration rules...")
	if checkAndEnforce(discoverConfDirs(synologyRoot)) {
		log.Println("Configuration updated.")
	} else {
		log.Println("Configuration already correct (or write failed).")
	}

	log.Println("Starting Synology Drive Client...")
	cmdOpen := exec.Command("open", "-a", "Synology Drive Client")
	if err := cmdOpen.Run(); err != nil {
		log.Printf("Failed to start Drive Client: %v", err)
	}

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

func notify(title, message string) {
	script := fmt.Sprintf("display notification %q with title %q", message, title)
	exec.Command("osascript", "-e", script).Run()
}

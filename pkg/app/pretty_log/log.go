package pretty_log

import (
	"fmt"
	"github.com/google/uuid"
	"strings"
	"sync"
	"time"
)

var (
	mut   sync.Mutex
	tasks = make(map[string]string)
)

// TaskGroup prints the title of a group of tasks in bright blue color.
func TaskGroup(format string, a ...interface{}) {
	title := fmt.Sprintf(format, a...)
	bold := "\033[1m"
	brightBlue := "\033[94m"
	reset := "\033[0m"
	now := time.Now().Format("2006/01/02 15:04:05")
	fmt.Printf("[%s] %s%s%s%s\n", now, brightBlue, bold, title, reset)
}

// BeginTask prints the beginning of a task with its name in orange and "..." in grey, without a newline at the end.
func BeginTask(format string, a ...interface{}) string {
	taskName := fmt.Sprintf(format, a...)
	orange := "\033[38;5;208m"
	grey := "\033[90m"
	reset := "\033[0m"

	mut.Lock()
	id := uuid.NewString()
	tasks[id] = taskName
	mut.Unlock()

	now := time.Now().Format("2006/01/02 15:04:05")
	fmt.Printf("[%s] %s%s%s %s%s\n", now, orange, taskName, reset, grey, reset)

	return id
}

// CompleteTask prints a green checkmark, then ends the line.
func CompleteTask(id string) {
	green := "\033[32m"
	reset := "\033[0m"

	mut.Lock()
	beginTask := tasks[id]
	delete(tasks, id)
	mut.Unlock()

	now := time.Now().Format("2006/01/02 15:04:05")
	fmt.Printf("[%s] %s%s%s%s%s\n", now, green, beginTask, reset, green, reset)
}

// FailTask prints a green checkmark, then ends the line.
func FailTask(id string) {
	red := "\033[31m"
	reset := "\033[0m"

	mut.Lock()
	beginTask := tasks[id]
	delete(tasks, id)
	mut.Unlock()

	now := time.Now().Format("2006/01/02 15:04:05")
	fmt.Printf("[%s] %s%s%s%s%s\n", now, red, beginTask, reset, red, reset)
}

// TaskResult prints the result of a task in cyan color.
func TaskResult(format string, a ...interface{}) {
	// Remove newline from the end of the format string
	format = strings.TrimSuffix(format, "\n")
	result := fmt.Sprintf(format, a...)
	cyan := "\033[36m"
	reset := "\033[0m"
	now := time.Now().Format("2006/01/02 15:04:05")
	fmt.Printf("[%s] %s%s%s\n", now, cyan, result, reset)
}

// TaskResultBad prints the result of a task in red color.
func TaskResultBad(format string, a ...interface{}) {
	// Remove newline from the end of the format string
	format = strings.TrimSuffix(format, "\n")
	result := fmt.Sprintf(format, a...)
	red := "\033[31m"
	reset := "\033[0m"
	now := time.Now().Format("2006/01/02 15:04:05")
	fmt.Printf("[%s] %s%s%s\n", now, red, result, reset)
}

// TaskResultList prints the result that is a string list
func TaskResultList(list []string) {
	cyan := "\033[36m"
	reset := "\033[0m"
	now := time.Now().Format("2006/01/02 15:04:05")

	for _, item := range list {
		if item == "" {
			continue
		}

		item = strings.TrimSuffix(item, "\n")

		fmt.Printf("[%s] %s - %s%s\n", now, cyan, item, reset)
	}
}

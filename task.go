package sup

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

// Task represents a set of commands to be run.
type Task struct {
	Run     string
	Input   io.Reader
	Clients []Client
	TTY     bool
}

func (sup *Stackup) createTasks(cmd *Command, clients []Client, env string) ([]*Task, error) {
	var tasks []*Task

	cwd, err := os.Getwd()
	if err != nil {
		return nil, errors.Wrap(err, "resolving CWD failed")
	}

	// Anything to upload?
	for _, upload := range cmd.Upload {

		var (
			uploadFiles = make(map[string][]int)
			uploadTars  = make(map[string]io.Reader)
			clientFile  = make([]string, len(clients))
		)

		for i := range clients {
			file, err := ResolveLocalPath(cwd, upload.Src, env+clients[i].Envs())
			if err != nil {
				return nil, errors.Wrap(err, "upload: "+upload.Src)
			}
			if strings.HasSuffix(upload.Src, "/") {
				file += "/"
			}
			uploadFiles[file] = append(uploadFiles[file], i)
			clientFile[i] = file
		}

		for file := range uploadFiles {
			basePath := filepath.Dir(file)
			baseFile := filepath.Base(file)
			if strings.HasSuffix(upload.Src, "/") {
				baseFile = "."
			}

			uploadTarReader, err := NewTarStreamReader(basePath, baseFile, upload.Exc)
			if err != nil {
				return nil, errors.Wrap(err, "upload: "+upload.Src)
			}
			uploadTars[file] = uploadTarReader
		}

		for file := range uploadTars {

			task := Task{
				Run:   RemoteTarCommand(upload.Dst),
				Input: uploadTars[file],
				TTY:   false,
			}

			if cmd.Once {
				task.Input = uploadTars[clientFile[0]]
				task.Clients = []Client{clients[0]}
				tasks = append(tasks, &task)
				break
			} else if cmd.Serial > 0 {
				if len(uploadTars) > 1 {
					return nil, errors.Wrap(fmt.Errorf("Serial not supported when using host variables"), "upload: "+upload.Src)
				}

				// Each "serial" task client group is executed sequentially.
				for i := 0; i < len(clients); i += cmd.Serial {
					j := i + cmd.Serial
					if j > len(clients) {
						j = len(clients)
					}
					copy := task
					copy.Clients = clients[i:j]
					tasks = append(tasks, &copy)
				}
			} else {
				if len(uploadTars) > 1 {
					for _, i := range uploadFiles[file] {
						task.Clients = append(task.Clients, clients[i])
					}
				} else {
					task.Clients = clients
				}
				tasks = append(tasks, &task)
			}
		}
	}

	// Script. Read the file as a multiline input command.
	if cmd.Script != "" {
		f, err := os.Open(cmd.Script)
		if err != nil {
			return nil, errors.Wrap(err, "can't open script")
		}
		data, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, errors.Wrap(err, "can't read script")
		}

		task := Task{
			Run: string(data),
			TTY: true,
		}
		if sup.debug {
			task.Run = "set -x;" + task.Run
		}
		if cmd.Stdin {
			task.Input = os.Stdin
		}
		if cmd.Once {
			task.Clients = []Client{clients[0]}
			tasks = append(tasks, &task)
		} else if cmd.Serial > 0 {
			// Each "serial" task client group is executed sequentially.
			for i := 0; i < len(clients); i += cmd.Serial {
				j := i + cmd.Serial
				if j > len(clients) {
					j = len(clients)
				}
				copy := task
				copy.Clients = clients[i:j]
				tasks = append(tasks, &copy)
			}
		} else {
			task.Clients = clients
			tasks = append(tasks, &task)
		}
	}

	// Local command.
	if cmd.Local != "" {
		local := &LocalhostClient{
			env: env + `export SUP_HOST="localhost";`,
		}
		local.Connect("localhost")
		task := &Task{
			Run:     cmd.Local,
			Clients: []Client{local},
			TTY:     true,
		}
		if sup.debug {
			task.Run = "set -x;" + task.Run
		}
		if cmd.Stdin {
			task.Input = os.Stdin
		}
		tasks = append(tasks, task)
	}

	// Remote command.
	if cmd.Run != "" {
		task := Task{
			Run: cmd.Run,
			TTY: true,
		}
		if sup.debug {
			task.Run = "set -x;" + task.Run
		}
		if cmd.Stdin {
			task.Input = os.Stdin
		}
		if cmd.Once {
			task.Clients = []Client{clients[0]}
			tasks = append(tasks, &task)
		} else if cmd.Serial > 0 {
			// Each "serial" task client group is executed sequentially.
			for i := 0; i < len(clients); i += cmd.Serial {
				j := i + cmd.Serial
				if j > len(clients) {
					j = len(clients)
				}
				copy := task
				copy.Clients = clients[i:j]
				tasks = append(tasks, &copy)
			}
		} else {
			task.Clients = clients
			tasks = append(tasks, &task)
		}
	}

	return tasks, nil
}

type ErrTask struct {
	Task   *Task
	Reason string
}

func (e ErrTask) Error() string {
	return fmt.Sprintf(`Run("%v"): %v`, e.Task, e.Reason)
}

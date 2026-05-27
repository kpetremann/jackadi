package sdk

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kpetremann/jackadi/internal/parser"
	"github.com/kpetremann/jackadi/internal/plugin/core"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/kpetremann/jackadi/internal/serializer"
)

var version = ""
var commit = ""
var date = ""

// getVersion returns plugin information.
//
// LDFLAGS (manually or via goreleaser) are used, with debug.RealBuildInfo as fallback.
func getVersion() core.Version {
	v := core.Version{
		PluginVersion: version,
		Commit:        commit,
		BuildTime:     date,
	}

	if version != "" && commit != "" && date != "" {
		return v
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}

	if v.PluginVersion == "" {
		v.PluginVersion = info.Main.Version
	}
	v.GoVersion = info.GoVersion
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if v.Commit == "" {
				v.Commit = setting.Value
			}
		case "vcs.time":
			if v.BuildTime == "" {
				v.BuildTime = setting.Value
			}
		}
	}

	return v
}

type Options interface {
	SetDefaults()
}

type Plugin struct {
	name      string
	tasks     map[string]*Task
	taskNames []string // to keep an ordered list

	specs      map[string]*SpecCollector
	specsNames []string
}

func New(name string) *Plugin {
	return &Plugin{
		name:  name,
		tasks: make(map[string]*Task),
		specs: make(map[string]*SpecCollector),
	}
}

func (t Plugin) Name() (string, error) { return t.name, nil }

func (t Plugin) Tasks() ([]string, error) {
	return t.taskNames, nil
}

func (t Plugin) Help(taskName string) (map[string]string, error) {
	res := make(map[string]string)

	// if no specific task, returns the short description (help) of every tasks
	if taskName == "" {
		var sb strings.Builder
		maxNameLen := 0
		for _, name := range t.taskNames {
			if len(name) > maxNameLen {
				maxNameLen = len(name)
			}
		}

		for _, name := range t.taskNames {
			task := t.tasks[name]
			if task == nil {
				slog.Error("failed to get help for task", "task", name, "error", "not found")
				continue
			}
			line := fmt.Sprintf("  %-*s", maxNameLen, task.name)

			if task.summary != "" {
				line += "  " + task.summary
			}

			if flags := task.flagsString(); flags != "" {
				line += fmt.Sprintf("  [%s]", flags)
			}

			sb.WriteString(line + "\n")
		}
		res[t.name] = sb.String()

		return res, nil
	}

	// if specific task, return the full details
	task, ok := t.tasks[taskName]
	if !ok {
		return nil, fmt.Errorf("unknown task: %s", taskName)
	}

	res[taskName] = task.helpText(t.name)
	return res, nil
}

func (t Plugin) Version() (core.Version, error) {
	return getVersion(), nil
}

func (t Plugin) GetTaskLockMode(taskName string) (proto.LockMode, error) {
	task, ok := t.tasks[taskName]
	if !ok {
		return proto.LockMode_NO_LOCK, fmt.Errorf("unknown task: %s", taskName)
	}
	return task.getLockMode().toProtoLockMode(), nil
}

func MustServe(plugin *Plugin) {
	if exit := handleFlags(plugin); exit {
		return
	}

	tasks, _ := plugin.Tasks()
	if len(tasks) == 0 {
		log.Fatalln("colletion must have at least one task")
	}

	// Run a task manually from the binary directly.
	handleCommand(plugin)

	cfg := goplugin.ServeConfig{
		HandshakeConfig: core.Handshake,
		Plugins: map[string]goplugin.Plugin{
			"plugin": &core.HCPlugin{Impl: plugin},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	}
	goplugin.Serve(&cfg)
}

func printCommandHelp() {
	fmt.Println("Usage: <plugin> run <command> [args...]")
	fmt.Println("       <plugin> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  task <task_name> [args...]  Run a specific task")
	fmt.Println("  specs                       Collect and display specs")
	fmt.Println()
	fmt.Println("Flags:")
	flag.PrintDefaults()
}

func handleCommand(plugin *Plugin) {
	if len(os.Args) >= 2 && os.Args[1] == "run" {
		if len(os.Args) < 3 {
			printCommandHelp()
			os.Exit(1)
		}
		switch os.Args[2] {
		case "task":
			if len(os.Args) < 4 {
				printCommandHelp()
				os.Exit(1)
			}
			arguments, err := parser.ParseArgs(os.Args[4:])
			if err != nil {
				fmt.Printf("failed to parse arguments: %s\n", err)
			}

			in, err := structpb.NewList(arguments.Positional)
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid arguments: %s\n", err)
				os.Exit(1)
			}

			opts, err := structpb.NewStruct(arguments.Options)
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid options: %s\n", err)
				os.Exit(1)
			}

			args := proto.Input{
				Args:    in,
				Options: opts,
			}

			resp, err := plugin.Do(context.Background(), os.Args[3], &args)
			if err != nil {
				fmt.Fprintf(os.Stderr, "task failed: %s\n", err)
				os.Exit(1)
			}
			if resp.Output != nil {
				var data any
				if err := serializer.JSON.UnmarshalFromString(string(resp.Output), &data); err != nil {
					fmt.Fprintf(os.Stderr, "unable to parse output: %s: %s\n", err, string(resp.Output)) // #nosec G705
					os.Exit(1)
				}
				out, _ := serializer.JSON.MarshalIndent(data, "", "  ")
				fmt.Println(string(out))
			}
			if resp.Error != "" {
				fmt.Println("error:", resp.Error)
			}
		case "specs":
			specs, specsErr := plugin.CollectSpecs(context.Background())
			var data any
			if err := serializer.JSON.UnmarshalFromString(string(specs), &data); err != nil {
				fmt.Fprintf(os.Stderr, "unable to parse output: %s: %s\n", err, string(specs))
				os.Exit(1)
			}
			out, _ := serializer.JSON.MarshalIndent(data, "", "  ")
			fmt.Println(string(out))

			if specsErr != nil {
				fmt.Println("error:", specsErr)
			}

		default:
			printCommandHelp()
			os.Exit(1)
		}
		os.Exit(0)
	}
}

// handleFlags processes command-line flags for the plugin and returns true if the plugin should exit.
func handleFlags(plugin *Plugin) bool {
	helpFlag := flag.Bool("help", false, "print command usage and available flags")
	versionFlag := flag.Bool("version", false, "print plugin information")
	describeFlag := flag.Bool("describe", false, "decribe plugin")
	flag.Parse()

	if *helpFlag {
		printCommandHelp()
		return true
	}

	if *versionFlag {
		fmt.Println(getVersion())
		return true
	}

	if *describeFlag {
		help, _ := plugin.Help("")
		for _, h := range help {
			fmt.Println(h)
		}
		return true
	}

	return false
}

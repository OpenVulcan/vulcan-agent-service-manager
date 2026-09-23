package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/appconfig"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/installation"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/pathenv"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/platform"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/release"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/selfupdate"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/service"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/state"
	"github.com/OpenVulcan/vulcan-agent-service-manager/internal/tui"
)

// main runs the CLI or TUI and reports an actionable error to the terminal.
// main 运行命令行或终端界面，并将可处理的错误报告给终端。
func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "vasm:", err)
		os.Exit(1)
	}
}

// run dispatches one manager command while sharing the same installer service with the TUI.
// run 分派一条管理器命令，同时与 TUI 共用同一个安装服务。
func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) >= 1 && args[0] == "--internal-apply-self-update" {
		if len(args) != 6 {
			return errors.New("invalid self-update helper arguments")
		}
		pid, err := strconv.Atoi(args[1])
		if err != nil {
			return err
		}
		return selfupdate.ApplyWindows(pid, args[2], args[3], args[4], args[5])
	}
	if executable, err := os.Executable(); err == nil {
		selfupdate.CleanupStale(executable)
	}
	if len(args) == 0 {
		return tui.Run(ctx, func(commandCtx context.Context, command []string, writer io.Writer) error {
			return run(commandCtx, command, writer)
		})
	}
	stateFile, err := state.DefaultFile()
	if err != nil {
		return err
	}
	client := release.NewClient()
	switch args[0] {
	case "--version", "version":
		if len(args) != 1 {
			return errors.New("version does not accept arguments")
		}
		fmt.Fprintln(output, "vasm", selfupdate.Version)
		return nil
	case "install":
		return installCommand(ctx, client, stateFile, args[1:], output)
	case "--internal-prepare-install":
		return prepareInstallCommand(ctx, client, args[1:], output)
	case "adopt":
		flags := flag.NewFlagSet("adopt", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("runtime-root", "", "existing release root")
		installed := flags.Bool("service-installed", false, "existing native service registration")
		defaultScope := "user"
		if runtime.GOOS == "windows" {
			defaultScope = "system"
		}
		scope := flags.String("scope", defaultScope, "user or system")
		name := flags.String("service-name", "VulcanAgentService", "existing native service name")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *root == "" || flags.NArg() != 0 {
			return errors.New("adopt requires --runtime-root <existing release root>")
		}
		record, err := installation.Adopt(ctx, stateFile, *root, *installed, *scope, *name)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "adopted %s at %s\n", record.AppTag, record.RuntimeRoot)
		return nil
	case "uninstall":
		flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		purge := flags.Bool("purge", false, "delete all service data")
		yes := flags.Bool("yes", false, "confirm full data removal")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (*purge && !*yes) {
			return errors.New("full data removal requires uninstall --purge --yes")
		}
		if err := installation.Uninstall(ctx, stateFile, *purge); err != nil {
			return err
		}
		fmt.Fprintln(output, "service program removed; user data retained:", !*purge)
		return nil
	case "update":
		record, err := state.Load(stateFile)
		if err != nil {
			return err
		}
		preferences, err := state.LoadPreferences(stateFile)
		if err != nil {
			return err
		}
		flags := flag.NewFlagSet("update", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		tag := flags.String("app-version", "", "service Release tag")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected update arguments")
		}
		options := installation.Options{StateFile: stateFile, RuntimeRoot: record.RuntimeRoot, Tag: *tag, Source: release.Source{Kind: preferences.Source, MirrorBase: preferences.MirrorBase}, UpgradeOnly: true}
		manager := installation.Manager{Releases: client}
		updated, err := manager.Install(ctx, options, progressPrinter(output))
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "service updated to %s from %s\n", updated.AppTag, sourceLabel(options.Source))
		return nil
	case "check-updates":
		return checkUpdates(ctx, client, stateFile, output)
	case "update-self":
		flags := flag.NewFlagSet("update-self", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		preferences, err := state.LoadPreferences(stateFile)
		if err != nil {
			return err
		}
		tag := flags.String("manager-version", "", "manager Release tag")
		sourceName := flags.String("source", preferences.Source, "github or mirror")
		mirrorBase := flags.String("mirror-base", preferences.MirrorBase, "HTTPS mirror proxy base")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected update-self arguments")
		}
		source, err := selectedSource(*sourceName, *mirrorBase)
		if err != nil {
			return err
		}
		updated, err := selfupdate.Update(ctx, client, source, *tag)
		if err != nil {
			return err
		}
		if updated == "v"+strings.TrimPrefix(selfupdate.Version, "v") {
			fmt.Fprintln(output, "manager is up to date:", updated)
			return nil
		}
		if runtime.GOOS == "windows" {
			if executable, err := os.Executable(); err == nil {
				if receipt, err := selfupdate.LastResult(executable); err == nil && receipt.Status == "pending" && receipt.Tag == updated {
					fmt.Fprintln(output, "manager update queued for process exit:", updated)
					return nil
				}
			}
		}
		fmt.Fprintf(output, "manager release: %s from %s\n", updated, sourceLabel(source))
		return nil
	case "status":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			return errors.New("status accepts only --json")
		}
		record, err := state.Load(stateFile)
		if err != nil {
			return err
		}
		if len(args) == 2 && args[1] == "--json" {
			return writeJSON(output, record)
		}
		fmt.Fprintf(output, "service: %s\nroot: %s\nsource: %s\n", record.AppTag, record.RuntimeRoot, record.Source)
		if record.ServiceInstalled {
			status, err := service.Lifecycle(ctx, record, "status")
			fmt.Fprint(output, status)
			return err
		}
		fmt.Fprintln(output, "mode: foreground")
		return nil
	case "service", "start", "stop", "restart":
		return serviceCommand(ctx, stateFile, args, output)
	case "run":
		if len(args) != 1 {
			return errors.New("run does not accept arguments")
		}
		record, err := state.Load(stateFile)
		if err != nil {
			return err
		}
		command := exec.CommandContext(ctx, service.Executable(record.RuntimeRoot), "--runtime-root", record.RuntimeRoot)
		command.Dir = record.RuntimeRoot
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		return command.Run()
	case "skills":
		return skillsCommand(ctx, stateFile, args[1:], output)
	case "config":
		return configCommand(stateFile, args[1:], output)
	case "source":
		return sourceCommand(stateFile, args[1:], output)
	case "path":
		return pathCommand(stateFile, args[1:], output)
	case "doctor":
		return doctorCommand(ctx, client, stateFile, args[1:], output)
	default:
		return fmt.Errorf("unknown command %q; run vasm for the TUI", args[0])
	}
}

// selectedSource resolves the explicit proxy preset without silently switching origins.
// selectedSource 解析显式代理预设，且不会暗中切换下载源。
func selectedSource(kind, base string) (release.Source, error) {
	if kind == "mirror" && base == "" {
		base = release.ChinaProxyBase
	}
	source := release.Source{Kind: kind, MirrorBase: base}
	return source, release.ValidateSource(source)
}

// sourceLabel describes the selected transfer endpoint in installation and update results.
// sourceLabel 在安装及更新结果中说明用户选择的传输端点。
func sourceLabel(source release.Source) string {
	if source.Kind == "mirror" {
		return source.MirrorBase
	}
	return "GitHub"
}

// installCommand parses explicit install choices and commits the selected application Release.
// installCommand 解析明确的安装选项并提交所选应用发布版本。
func installCommand(ctx context.Context, client *release.Client, stateFile string, args []string, output io.Writer) error {
	preferences, err := state.LoadPreferences(stateFile)
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sourceName := flags.String("source", preferences.Source, "github or mirror")
	mirrorBase := flags.String("mirror-base", preferences.MirrorBase, "HTTPS mirror proxy base")
	tag := flags.String("app-version", "", "service Release tag")
	root := flags.String("runtime-root", "", "absolute install path")
	yes := flags.Bool("yes", false, "confirm the selected installation")
	initSkills := flags.Bool("init-skills", false, "initialize enabled ROOT skills")
	skillSelection := flags.String("skills", "default", "default, none, or comma-separated ROOT skill names")
	installService := flags.Bool("service", false, "register native service")
	defaultScope := "user"
	if runtime.GOOS == "windows" {
		defaultScope = "system"
	}
	scope := flags.String("scope", defaultScope, "user or system")
	startup := flags.String("startup", "manual", "auto or manual")
	start := flags.Bool("start", false, "start service after registration")
	addPath := flags.Bool("add-path", false, "add the manager command directory to user PATH")
	preparedArchive := flags.String("prepared-archive", "", "already downloaded archive verified against the selected Release")
	vmm := flags.String("vmm", "unchanged", "true, false, or unchanged")
	vmmURL := flags.String("vmm-url", "", "VMM endpoint")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected install arguments")
	}
	if !*yes {
		return errors.New("noninteractive installation requires --yes; run vasm for the guided TUI")
	}
	if *root == "" {
		var err error
		*root, err = state.DefaultRuntimeRoot()
		if err != nil {
			return err
		}
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	source, err := selectedSource(*sourceName, *mirrorBase)
	if err != nil {
		return err
	}
	options := installation.Options{StateFile: stateFile, RuntimeRoot: absRoot, Tag: *tag, Source: source, InitializeSkills: *initSkills, VMMEndpoint: *vmmURL, InstallService: *installService, ServiceScope: *scope, Startup: *startup, StartService: *start, PreparedArchive: *preparedArchive}
	if *skillSelection == "none" {
		options.SkillNames = []string{}
	} else if *skillSelection != "default" {
		options.SkillNames = strings.Split(*skillSelection, ",")
		for index := range options.SkillNames {
			options.SkillNames[index] = strings.TrimSpace(options.SkillNames[index])
		}
	}
	if *vmm == "true" || *vmm == "false" {
		enabled := *vmm == "true"
		options.VMMEnabled = &enabled
	} else if *vmm != "unchanged" {
		return errors.New("--vmm must be true, false, or unchanged")
	}
	manager := installation.Manager{Releases: client}
	record, err := manager.Install(ctx, options, progressPrinter(output))
	if err != nil {
		return err
	}
	preferences.Source, preferences.MirrorBase = source.Kind, source.MirrorBase
	if err := state.SavePreferences(stateFile, preferences); err != nil {
		return fmt.Errorf("service installed but manager preferences could not be saved: %w", err)
	}
	fmt.Fprintf(output, "installed %s at %s from %s\n", record.AppTag, record.RuntimeRoot, sourceLabel(source))
	if *addPath {
		if err := pathCommand(stateFile, []string{"add"}, output); err != nil {
			return fmt.Errorf("service installed, but PATH setup failed: %w", err)
		}
	}
	return nil
}

// prepareInstallCommand fetches and verifies a service archive before the TUI asks for installation settings.
// prepareInstallCommand 在 TUI 询问安装配置之前下载并校验主程序归档。
func prepareInstallCommand(ctx context.Context, client *release.Client, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("prepare-install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	sourceName := flags.String("source", "github", "github or mirror")
	mirrorBase := flags.String("mirror-base", "", "HTTPS mirror proxy base")
	tag := flags.String("app-version", "", "service Release tag")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected prepare-install arguments")
	}
	source, err := selectedSource(*sourceName, *mirrorBase)
	if err != nil {
		return err
	}
	info, err := client.Fetch(ctx, release.ServiceRepository, *tag)
	if err != nil {
		return err
	}
	target, err := platform.Current()
	if err != nil {
		return err
	}
	asset, err := info.FindAsset(platform.AppAssetName(info.Tag, target))
	if err != nil {
		return err
	}
	workspace, err := os.MkdirTemp("", ".vasm-prepared-*")
	if err != nil {
		return err
	}
	archivePath := filepath.Join(workspace, asset.Name)
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(archivePath)
			_ = os.Remove(workspace)
		}
	}()
	if err := client.Download(ctx, source, asset, archivePath, nil); err != nil {
		return err
	}
	if err := writeJSON(output, map[string]any{"tag": info.Tag, "archive_path": archivePath, "asset_name": asset.Name, "asset_size": asset.Size, "source": source.Kind}); err != nil {
		return err
	}
	keep = true
	return nil
}

// checkUpdates reports both independent latest Releases against locally installed versions.
// checkUpdates 对照本地已安装版本报告两个独立的最新发布版本。
func checkUpdates(ctx context.Context, client *release.Client, stateFile string, output io.Writer) error {
	managerRelease, managerErr := selfupdate.Check(ctx, client, "")
	serviceRelease, serviceErr := client.Fetch(ctx, release.ServiceRepository, "")
	installedTag := ""
	if record, err := state.Load(stateFile); err == nil {
		installedTag = record.AppTag
	}
	result := map[string]any{"manager_installed": "v" + strings.TrimPrefix(selfupdate.Version, "v"), "service_installed": installedTag}
	if executable, err := os.Executable(); err == nil {
		if receipt, err := selfupdate.LastResult(executable); err == nil {
			result["manager_last_update"] = receipt
		}
	}
	if managerErr == nil {
		result["manager_latest"] = managerRelease.Tag
		if comparison, err := release.CompareTags(managerRelease.Tag, "v"+strings.TrimPrefix(selfupdate.Version, "v")); err == nil {
			result["manager_update_available"] = comparison > 0
		}
	} else {
		result["manager_error"] = managerErr.Error()
	}
	if serviceErr == nil {
		result["service_latest"] = serviceRelease.Tag
		if installedTag != "" {
			if comparison, err := release.CompareTags(serviceRelease.Tag, installedTag); err == nil {
				result["service_update_available"] = comparison > 0
			}
		}
	} else {
		result["service_error"] = serviceErr.Error()
	}
	return writeJSON(output, result)
}

// serviceCommand routes native service actions through the installed service binary.
// serviceCommand 通过已安装的服务程序路由本机服务操作。
func serviceCommand(ctx context.Context, stateFile string, args []string, output io.Writer) error {
	action := args[0]
	if action == "service" {
		if len(args) < 2 {
			return errors.New("service requires install, uninstall, or status")
		}
		action = args[1]
	} else if len(args) != 1 {
		return errors.New("service lifecycle action does not accept arguments")
	}
	if action != "status" {
		unlock, err := state.Lock(stateFile)
		if err != nil {
			return err
		}
		defer unlock()
	}
	record, err := state.Load(stateFile)
	if err != nil {
		return err
	}
	switch action {
	case "install":
		if record.ServiceInstalled {
			return errors.New("native service is already installed")
		}
		flags := flag.NewFlagSet("service install", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		defaultScope := "user"
		if runtime.GOOS == "windows" {
			defaultScope = "system"
		}
		scope := flags.String("scope", defaultScope, "user or system")
		startup := flags.String("startup", "manual", "auto or manual")
		start := flags.Bool("start", false, "start after installation")
		if err := flags.Parse(args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected service install arguments")
		}
		record.ServiceScope = *scope
		if runtime.GOOS == "windows" && record.ServiceScope != "system" {
			return errors.New("Windows native service requires system scope")
		}
		text, err := service.Install(ctx, record, *startup, *start)
		if err != nil {
			return err
		}
		record.ServiceInstalled = true
		record.Startup = *startup
		if err := state.Save(stateFile, record); err != nil {
			_, _ = service.Lifecycle(ctx, record, "uninstall")
			return err
		}
		fmt.Fprint(output, text)
		return nil
	case "uninstall":
		if len(args) != 2 {
			return errors.New("service uninstall does not accept arguments")
		}
		text, err := service.Lifecycle(ctx, record, "uninstall")
		if err != nil {
			return err
		}
		record.ServiceInstalled = false
		if err := state.Save(stateFile, record); err != nil {
			return err
		}
		fmt.Fprint(output, text)
		return nil
	case "startup":
		if len(args) != 3 {
			return errors.New("service startup requires auto or manual")
		}
		text, err := service.SetStartup(ctx, record, args[2])
		if err != nil {
			return err
		}
		record.Startup = args[2]
		if err := state.Save(stateFile, record); err != nil {
			return err
		}
		fmt.Fprint(output, text)
		return nil
	case "start", "stop", "restart", "status":
		if args[0] == "service" && len(args) != 2 {
			return errors.New("service lifecycle action does not accept arguments")
		}
		text, err := service.Lifecycle(ctx, record, action)
		fmt.Fprint(output, text)
		return err
	default:
		return fmt.Errorf("unsupported service action %q", action)
	}
}

// skillsCommand delegates ROOT and USER skill actions to verified service commands.
// skillsCommand 将 ROOT 和 USER 技能操作转发给已核实的服务命令。
func skillsCommand(ctx context.Context, stateFile string, args []string, output io.Writer) error {
	if len(args) < 1 {
		return errors.New("skills requires list, install, update, or uninstall")
	}
	action := args[0]
	layer := "USER"
	positional := args[1:]
	if len(positional) >= 2 && positional[len(positional)-2] == "--layer" {
		layer = strings.ToUpper(positional[len(positional)-1])
		positional = positional[:len(positional)-2]
	}
	if layer != "ROOT" && layer != "USER" {
		return errors.New("skill layer must be ROOT or USER")
	}
	for _, value := range positional {
		if value == "" || strings.HasPrefix(value, "--") {
			return errors.New("skill positional arguments must be nonempty and precede --layer")
		}
	}
	expectedArgs := 0
	switch action {
	case "list":
	case "install":
		expectedArgs = 1
	case "update":
		if layer == "USER" {
			expectedArgs = 1
		}
	case "uninstall":
		if layer == "ROOT" {
			return errors.New("ROOT layer does not support skill uninstall")
		}
		expectedArgs = 1
	default:
		return fmt.Errorf("unsupported skill action %q", action)
	}
	if len(positional) != expectedArgs {
		return fmt.Errorf("unexpected arguments for %s skill action %s", layer, action)
	}
	record, err := state.Load(stateFile)
	if err != nil {
		return err
	}
	if layer == "ROOT" {
		switch action {
		case "list":
			contents, err := os.ReadFile(filepath.Join(record.RuntimeRoot, "configs", "system_skills.json"))
			if err != nil {
				return err
			}
			_, err = output.Write(contents)
			return err
		case "install":
			text, err := service.Run(ctx, record.RuntimeRoot, "--install-root-skill", positional[0], "--source-type", "github", "--runtime-root", record.RuntimeRoot)
			fmt.Fprint(output, text)
			return err
		case "update":
			text, err := service.Run(ctx, record.RuntimeRoot, "--update-root-skills", "--runtime-root", record.RuntimeRoot)
			fmt.Fprint(output, text)
			return err
		default:
			return errors.New("ROOT layer only supports list, install, and update-all")
		}
	}
	request := map[string]any{"action": action}
	if action == "install" {
		request["source"] = positional[0]
		request["source_type"] = "github"
	} else if action == "update" || action == "uninstall" {
		request["skill_id"] = positional[0]
	} else if action != "list" {
		return fmt.Errorf("unsupported skill action %q", action)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	text, err := service.Run(ctx, record.RuntimeRoot, "--call-tools", "skill-manager", string(encoded), "--runtime-root", record.RuntimeRoot)
	fmt.Fprint(output, text)
	return err
}

// configCommand exposes only fields with confirmed service configuration ownership.
// configCommand 仅开放已确认由服务配置拥有的字段。
func configCommand(stateFile string, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("config requires show, set, budget, or skill")
	}
	if args[0] != "show" {
		unlock, err := state.Lock(stateFile)
		if err != nil {
			return err
		}
		defer unlock()
	}
	record, err := state.Load(stateFile)
	if err != nil {
		return err
	}
	switch args[0] {
	case "show":
		if len(args) != 1 && !(len(args) == 2 && args[1] == "--json") {
			return errors.New("config show accepts only --json")
		}
		values, err := appconfig.Show(record.RuntimeRoot)
		if err != nil {
			return err
		}
		return writeJSON(output, values)
	case "set":
		if len(args) != 3 {
			return errors.New("config set requires <key> <value>")
		}
		return appconfig.SetScalar(record.RuntimeRoot, appconfig.Scalar{Key: args[1], Value: args[2]})
	case "budget":
		if len(args) != 3 {
			return errors.New("config budget requires <client-pattern|default> <bytes>")
		}
		limit, err := strconv.Atoi(args[2])
		if err != nil {
			return err
		}
		pattern := args[1]
		if pattern == "default" {
			pattern = ""
		}
		return appconfig.SetToolResultBytes(record.RuntimeRoot, pattern, limit)
	case "skill":
		if len(args) != 3 || (args[2] != "true" && args[2] != "false") {
			return errors.New("config skill requires <name|@auto-install> <true|false>")
		}
		return appconfig.SetSkillEnabled(record.RuntimeRoot, args[1], args[2] == "true")
	default:
		return fmt.Errorf("unsupported config action %q", args[0])
	}
}

// sourceCommand shows or changes the persisted application download source.
// sourceCommand 显示或修改持久化的应用下载源。
func sourceCommand(stateFile string, args []string, output io.Writer) error {
	if len(args) != 1 || args[0] != "show" {
		unlock, err := state.Lock(stateFile)
		if err != nil {
			return err
		}
		defer unlock()
	}
	preferences, err := state.LoadPreferences(stateFile)
	if err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "show" {
		return writeJSON(output, map[string]string{"source": preferences.Source, "mirror_base": preferences.MirrorBase})
	}
	if (len(args) != 2 && len(args) != 3) || args[0] != "set" {
		return errors.New("source requires show or set <github|mirror> [mirror-base]")
	}
	base := ""
	if len(args) > 2 {
		base = args[2]
	}
	source, err := selectedSource(args[1], base)
	if err != nil {
		return err
	}
	preferences.Source, preferences.MirrorBase = source.Kind, source.MirrorBase
	return state.SavePreferences(stateFile, preferences)
}

// doctorCommand checks local layout and the public service Release endpoint.
// doctorCommand 检查本地布局及公开的服务发布端点。
func doctorCommand(ctx context.Context, client *release.Client, stateFile string, args []string, output io.Writer) error {
	result := map[string]any{"manager_version": selfupdate.Version, "checked_at": time.Now().UTC().Format(time.RFC3339)}
	target, err := platform.Current()
	if err != nil {
		result["platform_error"] = err.Error()
	} else {
		result["platform"] = target.Name
	}
	if record, err := state.Load(stateFile); err == nil {
		result["service_installed"] = true
		result["runtime_root"] = record.RuntimeRoot
		result["service_binary_exists"] = fileExists(service.Executable(record.RuntimeRoot))
		result["app_tag"] = record.AppTag
	} else if errors.Is(err, os.ErrNotExist) {
		result["service_installed"] = false
	} else {
		result["state_error"] = err.Error()
	}
	if info, err := client.Fetch(ctx, release.ServiceRepository, ""); err == nil {
		result["service_latest"] = info.Tag
	} else {
		result["release_error"] = err.Error()
	}
	return writeJSON(output, result)
}

// fileExists reports whether a regular file exists at a known path.
// fileExists 报告已知路径是否存在普通文件。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// writeJSON writes a stable, indented machine-readable command response.
// writeJSON 写出稳定且缩进的机器可读命令响应。
func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// pathCommand manages only a PATH entry added by the current manager installation.
// pathCommand 仅管理当前管理器安装自身添加的 PATH 条目。
func pathCommand(stateFile string, args []string, output io.Writer) error {
	if len(args) != 1 {
		return errors.New("path requires add, remove, or show")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	directory := filepath.Dir(executable)
	if args[0] == "show" {
		return writeJSON(output, map[string]string{"manager_directory": directory, "note": "new terminals read updated PATH"})
	}
	unlock, err := state.Lock(stateFile)
	if err != nil {
		return err
	}
	defer unlock()
	preferences, err := state.LoadPreferences(stateFile)
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		if preferences.PathAdded && preferences.PathDirectory != directory {
			return fmt.Errorf("manager PATH already belongs to %s; remove it before adding another location", preferences.PathDirectory)
		}
		added, err := pathenv.Add(directory)
		if err != nil {
			return err
		}
		if added {
			preferences.PathAdded = true
			preferences.PathDirectory = directory
			if err := state.SavePreferences(stateFile, preferences); err != nil {
				_ = pathenv.Remove(directory)
				return err
			}
		}
		fmt.Fprintf(output, "manager PATH entry: %s (added=%t; open a new terminal)\n", directory, added)
		return nil
	case "remove":
		if !preferences.PathAdded {
			return errors.New("manager did not add this PATH entry")
		}
		if err := pathenv.Remove(preferences.PathDirectory); err != nil {
			return err
		}
		preferences.PathAdded = false
		preferences.PathDirectory = ""
		return state.SavePreferences(stateFile, preferences)
	default:
		return errors.New("path requires add, remove, or show")
	}
}

// progressPrinter emits stage changes and five-percent download milestones without flooding terminals.
// progressPrinter 仅输出阶段变化与每百分之五的下载节点，避免刷满终端。
func progressPrinter(output io.Writer) func(string, int64, int64) {
	lastStage := ""
	lastBucket := -1
	return func(stage string, done, total int64) {
		bucket := 0
		if total > 0 {
			bucket = int(done * 20 / total)
		}
		if stage == lastStage && bucket == lastBucket {
			return
		}
		lastStage, lastBucket = stage, bucket
		if total > 0 {
			fmt.Fprintf(output, "%s %d%%\n", stage, bucket*5)
		} else {
			fmt.Fprintln(output, stage)
		}
	}
}

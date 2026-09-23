package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

// Runner executes one equivalent CLI command and returns its output.
// Runner 执行一条等价的命令行命令并返回其输出。
type Runner func(context.Context, []string, io.Writer) error

// model holds the current menu, installation choices, and asynchronous result.
// model 保存当前菜单、安装选项和异步操作结果。
type model struct {
	// ctx cancels downloads and child commands when the TUI exits.
	// ctx 在 TUI 退出时取消下载及子命令。
	ctx context.Context
	// cancel stops all work owned by this TUI session.
	// cancel 停止当前 TUI 会话拥有的全部工作。
	cancel context.CancelFunc
	// runner is the shared CLI command dispatcher.
	// runner 是共用的命令行分派器。
	runner Runner
	// page is the currently displayed management screen.
	// page 是当前显示的管理页面。
	page string
	// cursor is the selected menu or wizard row.
	// cursor 是当前选中的菜单或向导行。
	cursor int
	// source selects github, China proxy, or custom proxy.
	// source 选择 GitHub、国内代理预设或自定义代理。
	source int
	// version selects latest or a custom tag.
	// version 选择最新版或自定义标签。
	version int
	// vmm selects whether to enable VMM integration.
	// vmm 选择是否启用 VMM 集成。
	vmm bool
	// vmmURL optionally overrides the published VMM endpoint during installation.
	// vmmURL 可选地在安装时覆盖发布包中的 VMM 地址。
	vmmURL string
	// mode selects foreground, user service, or system service.
	// mode 选择前台、用户服务或系统服务。
	mode int
	// autostart selects native service automatic startup.
	// autostart 选择本机服务自动启动。
	autostart bool
	// initSkills selects initial ROOT skill installation.
	// initSkills 选择初始化 ROOT 技能安装。
	initSkills bool
	// skillNames is default, none, or comma-separated ROOT skill names for first install.
	// skillNames 是首次安装时的 default、none 或逗号分隔的 ROOT 技能名称。
	skillNames string
	// addPath selects whether vasm adds its command directory to user PATH.
	// addPath 选择是否由 vasm 将命令目录加入用户 PATH。
	addPath bool
	// mirror is a custom HTTPS proxy base.
	// mirror 是自定义 HTTPS 代理基址。
	mirror string
	// tag is a custom application Release tag.
	// tag 是自定义应用发布标签。
	tag string
	// preparedTag pins the exact official Release selected during the first download phase.
	// preparedTag 固定首次下载阶段选定的官方发布标签。
	preparedTag string
	// preparedArchive is the verified package retained until installation or TUI exit.
	// preparedArchive 是保留至安装或退出 TUI 的已校验归档。
	preparedArchive string
	// preparedAsset is the exact published archive name shown before configuration.
	// preparedAsset 是配置前展示的准确发布归档名称。
	preparedAsset string
	// preparedBytes is the official published archive size shown to the user.
	// preparedBytes 是向用户展示的官方发布归档字节数。
	preparedBytes int64
	// pendingPrepare marks the current operation as the first download phase.
	// pendingPrepare 标记当前操作属于首次下载阶段。
	pendingPrepare bool
	// pendingInstall marks the current operation as the commit of a prepared package.
	// pendingInstall 标记当前操作正在提交已预取的发布包。
	pendingInstall bool
	// root is an optional absolute install destination.
	// root 是可选的安装目标绝对路径。
	root string
	// inputLabel names the currently edited field.
	// inputLabel 标识当前编辑的字段。
	inputLabel string
	// inputText is the current editable text.
	// inputText 是当前可编辑文本。
	inputText string
	// inputCommand is the command prefix used for a management text field.
	// inputCommand 是管理文本字段的命令前缀。
	inputCommand []string
	// result is the last completed operation output.
	// result 是最近一次完成操作的输出。
	result string
	// errorText is the last completed operation error.
	// errorText 是最近一次完成操作的错误。
	errorText string
	// height is the terminal height used for result clipping.
	// height 是用于裁切结果的终端高度。
	height int
	// resultOffset is the first visible result line.
	// resultOffset 是第一条可见结果行的偏移。
	resultOffset int
	// pendingCommand is the operation waiting for a TUI confirmation.
	// pendingCommand 是等待 TUI 确认的操作。
	pendingCommand []string
	// pendingLabel explains the confirmed operation to the user.
	// pendingLabel 向用户解释待确认操作。
	pendingLabel string
	// progress is the latest line from a running operation.
	// progress 是运行中操作的最新输出行。
	progress string
	// events carries background progress and completion back to Bubble Tea.
	// events 将后台进度及完成消息传回 Bubble Tea。
	events chan tea.Msg
}

// operationResult is the message returned by one asynchronous manager command.
// operationResult 是一条异步管理命令返回的消息。
type operationResult struct {
	// output is the captured command output.
	// output 是捕获的命令输出。
	output string
	// err is the command failure, if any.
	// err 是命令错误，如有。
	err error
}

// progressResult is a single line emitted by the shared CLI operation.
// progressResult 是共用命令行操作输出的单行进度。
type progressResult struct {
	// text is the current human-readable stage.
	// text 是当前可读阶段。
	text string
}

// eventWriter collects command output and forwards progress to the active TUI.
// eventWriter 收集命令输出并将进度转发给当前 TUI。
type eventWriter struct {
	// contents is the full result shown after completion.
	// contents 是完成后展示的完整结果。
	contents strings.Builder
	// events is the current operation's message channel.
	// events 是当前操作的消息通道。
	events chan tea.Msg
}

// Write records a CLI output chunk and publishes its latest visible line.
// Write 记录一段命令行输出并发布其最新可见行。
func (writer *eventWriter) Write(content []byte) (int, error) {
	count, err := writer.contents.Write(content)
	if count > 0 {
		writer.events <- progressResult{text: strings.TrimSpace(string(content))}
	}
	return count, err
}

// Run opens the manager terminal interface until the user exits.
// Run 打开管理器终端界面，直到用户退出；管道执行时改用真实控制台。
func Run(parent context.Context, runner Runner) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	initial := model{ctx: ctx, cancel: cancel, runner: runner, page: "home", height: 24, initSkills: true, skillNames: "default"}
	var savedOutput strings.Builder
	if err := runner(ctx, []string{"source", "show"}, &savedOutput); err != nil {
		return fmt.Errorf("cannot load manager download source: %w", err)
	}
	var saved struct {
		// Source is the persisted transfer preference.
		// Source 是已持久化的传输偏好。
		Source string `json:"source"`
		// MirrorBase is the persisted custom HTTPS proxy prefix.
		// MirrorBase 是已持久化的自定义 HTTPS 代理前缀。
		MirrorBase string `json:"mirror_base"`
	}
	if err := json.Unmarshal([]byte(savedOutput.String()), &saved); err != nil {
		return fmt.Errorf("cannot decode manager download source: %w", err)
	}
	if saved.Source == "mirror" {
		initial.source = 1
		if saved.MirrorBase != "" && saved.MirrorBase != "https://gh-proxy.com" {
			initial.source = 2
			initial.mirror = saved.MirrorBase
		}
	}
	if os.Getenv("VASM_SOURCE") == "mirror" {
		initial.source = 1
		if custom := os.Getenv("VASM_MIRROR_BASE"); custom != "" && custom != "https://gh-proxy.com" {
			initial.source = 2
			initial.mirror = custom
		}
	}
	input, output, closeTerminal, err := terminalIO()
	if err != nil {
		return err
	}
	defer closeTerminal()
	program := tea.NewProgram(initial, tea.WithInput(input), tea.WithOutput(output))
	final, err := program.Run()
	cancel()
	if last, ok := final.(model); ok {
		cleanupPreparedArchive(last.preparedArchive)
	}
	return err
}

// terminalIO attaches a piped bootstrap to the controlling terminal or explains why the TUI cannot start.
// terminalIO 将管道引导流程接到控制终端；若没有控制终端则明确说明 TUI 无法启动。
func terminalIO() (io.Reader, io.Writer, func(), error) {
	input := os.Stdin
	output := os.Stdout
	opened := make([]*os.File, 0, 2)
	closeOpened := func() {
		for _, file := range opened {
			_ = file.Close()
		}
	}
	inputName, outputName := "/dev/tty", "/dev/tty"
	if runtime.GOOS == "windows" {
		inputName, outputName = "CONIN$", "CONOUT$"
	}
	if !term.IsTerminal(input.Fd()) {
		console, err := os.Open(inputName)
		if err != nil {
			return nil, nil, closeOpened, errors.New("interactive terminal unavailable; use vasm install --yes for a non-interactive installation")
		}
		opened = append(opened, console)
		input = console
	}
	if !term.IsTerminal(output.Fd()) {
		console, err := os.OpenFile(outputName, os.O_WRONLY, 0)
		if err != nil {
			closeOpened()
			return nil, nil, func() {}, errors.New("interactive terminal output unavailable; use vasm install --yes for a non-interactive installation")
		}
		opened = append(opened, console)
		output = console
	}
	if !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		closeOpened()
		return nil, nil, func() {}, errors.New("interactive terminal is required; use vasm install --yes for a non-interactive installation")
	}
	return input, output, closeOpened, nil
}

// cleanupPreparedArchive removes only a TUI-owned archive and its empty temporary directory.
// cleanupPreparedArchive 只删除 TUI 持有的归档及其空暂存目录。
func cleanupPreparedArchive(archivePath string) {
	if archivePath == "" || !strings.HasPrefix(filepath.Base(filepath.Dir(archivePath)), ".vasm-prepared-") {
		return
	}
	_ = os.Remove(archivePath)
	_ = os.Remove(filepath.Dir(archivePath))
}

// Init supplies no automatic command because network work requires a user choice.
// Init 不自动执行命令，因为网络操作需要用户先做选择。
func (m model) Init() tea.Cmd {
	return nil
}

// Update applies navigation, field edits, and completed command messages.
// Update 应用导航、字段编辑及已完成命令消息。
func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch item := message.(type) {
	case tea.WindowSizeMsg:
		m.height = item.Height
		return m, nil
	case operationResult:
		if m.pendingPrepare {
			m.pendingPrepare = false
			if item.err == nil {
				var prepared struct {
					// Tag is the exact downloaded application Release.
					// Tag 是已下载应用程序的准确发布标签。
					Tag string `json:"tag"`
					// ArchivePath is the temporary verified package path.
					// ArchivePath 是暂存的已校验归档路径。
					ArchivePath string `json:"archive_path"`
					// AssetName is the exact published archive filename.
					// AssetName 是准确的发布归档文件名。
					AssetName string `json:"asset_name"`
					// AssetSize is the official published archive size.
					// AssetSize 是官方发布归档大小。
					AssetSize int64 `json:"asset_size"`
				}
				if err := json.Unmarshal([]byte(item.output), &prepared); err == nil && prepared.Tag != "" && prepared.ArchivePath != "" && prepared.AssetName != "" && prepared.AssetSize > 0 {
					m.preparedTag = prepared.Tag
					m.preparedArchive = prepared.ArchivePath
					m.preparedAsset = prepared.AssetName
					m.preparedBytes = prepared.AssetSize
					m.page, m.cursor = "wizard", 0
					m.events = nil
					return m, nil
				}
				item.err = fmt.Errorf("cannot decode the verified package result")
			}
		}
		if m.pendingInstall {
			m.pendingInstall = false
			if item.err == nil {
				cleanupPreparedArchive(m.preparedArchive)
				m.preparedArchive, m.preparedTag, m.preparedAsset, m.preparedBytes = "", "", "", 0
			}
		}
		m.page = "result"
		m.result = item.output
		m.errorText = ""
		if item.err != nil {
			m.errorText = item.err.Error()
		}
		m.resultOffset = 0
		m.events = nil
		return m, nil
	case progressResult:
		m.progress = item.text
		return m, waitEvent(m.events)
	case tea.KeyPressMsg:
		key := item.String()
		if key == "ctrl+c" {
			m.cancel()
			return m, tea.Quit
		}
		if m.page == "input" {
			return m.updateInput(item)
		}
		if m.page == "working" {
			return m, nil
		}
		if m.page == "confirm" {
			if key == "y" || key == "Y" {
				return m.execute(m.pendingCommand)
			}
			if key == "n" || key == "N" || key == "esc" {
				m.pendingInstall = false
				m.page, m.cursor = "home", 0
			}
			return m, nil
		}
		if m.page == "result" {
			return m.updateResult(key)
		}
		if key == "esc" || key == "q" {
			if m.page == "home" {
				m.cancel()
				return m, tea.Quit
			}
			if m.page == "wizard" && m.preparedTag != "" {
				cleanupPreparedArchive(m.preparedArchive)
				m.preparedArchive, m.preparedTag, m.preparedAsset, m.preparedBytes = "", "", "", 0
				m.cursor = 0
				return m, nil
			}
			m.page, m.cursor = "home", 0
			return m, nil
		}
		if m.page == "wizard" {
			return m.updateWizard(key)
		}
		menu := menuFor(m.page)
		if key == "up" || key == "k" {
			m.cursor = (m.cursor + len(menu) - 1) % len(menu)
		} else if key == "down" || key == "j" {
			m.cursor = (m.cursor + 1) % len(menu)
		} else if key == "enter" {
			return m.selectMenu()
		}
	}
	return m, nil
}

// View renders the selected menu, wizard, input field, or operation result.
// View 渲染选中的菜单、向导、输入字段或操作结果。
func (m model) View() tea.View {
	var builder strings.Builder
	builder.WriteString("Vulcan Agent Service Manager  •  vasm\n")
	builder.WriteString("────────────────────────────────────────\n")
	switch m.page {
	case "wizard":
		builder.WriteString("安装向导  ·  ↑↓选择，空格切换，Enter 确认\n")
		if m.preparedTag != "" {
			source := "GitHub 官方"
			if m.source == 1 {
				source = "国内代理预设"
			} else if m.source == 2 {
				source = m.mirror
			}
			builder.WriteString(fmt.Sprintf("已校验主程序包: %s  ·  %s (%d 字节)  ·  下载源: %s\n", m.preparedTag, m.preparedAsset, m.preparedBytes, source))
		}
		builder.WriteString("\n")
		for index, row := range m.wizardRows() {
			prefix := "  "
			if index == m.cursor {
				prefix = "› "
			}
			builder.WriteString(prefix + row + "\n")
		}
	case "input":
		builder.WriteString(m.inputLabel + "\n\n" + m.inputText + "▌\n\nEnter 保存  Esc 取消\n")
	case "confirm":
		builder.WriteString(m.pendingLabel + "\n\n按 Y 确认，按 N 取消。\n")
	case "working":
		builder.WriteString("正在执行，请等待下载、校验和提交完成……\n\n" + m.progress + "\n")
	case "result":
		builder.WriteString("操作结果  ·  ↑↓滚动，Enter 返回首页\n\n")
		lines := strings.Split(m.result, "\n")
		visible := m.height - 8
		if visible < 5 {
			visible = 5
		}
		end := m.resultOffset + visible
		if end > len(lines) {
			end = len(lines)
		}
		for _, line := range lines[m.resultOffset:end] {
			builder.WriteString(line + "\n")
		}
		if m.errorText != "" {
			builder.WriteString("\n错误: " + m.errorText + "\n")
		}
	default:
		builder.WriteString(pageTitle(m.page) + "  ·  ↑↓选择，Enter 执行，Esc 返回\n\n")
		for index, row := range menuFor(m.page) {
			prefix := "  "
			if index == m.cursor {
				prefix = "› "
			}
			builder.WriteString(prefix + row + "\n")
		}
	}
	view := tea.NewView(builder.String())
	view.AltScreen = true
	view.WindowTitle = "Vulcan Agent Service Manager"
	return view
}

// menuFor returns the fixed, verified actions for one management page.
// menuFor 返回某一管理页面中固定且已核实的操作。
func menuFor(page string) []string {
	switch page {
	case "service":
		return []string{"查看服务状态", "启动服务", "停止服务", "重启服务", "安装为服务", "设置开机自启", "设置手动启动", "卸载服务", "返回首页"}
	case "skills":
		return []string{"查看 ROOT 系统技能", "安装 ROOT 系统技能", "更新全部 ROOT 技能", "查看 USER 技能", "安装 USER 技能", "更新 USER 技能", "卸载 USER 技能", "返回首页"}
	case "config":
		return []string{"查看当前配置", "启用 VMM", "禁用 VMM", "设置 VMM 地址", "设置默认工具结果字节上限", "按请求头值设置工具结果字节上限", "启用自动安装系统技能", "禁用自动安装系统技能", "启用指定系统技能", "禁用指定系统技能", "返回首页"}
	case "source":
		return []string{"查看下载源", "使用 GitHub 官方源", "使用国内代理预设", "设置自定义 HTTPS 镜像", "返回首页"}
	case "path":
		return []string{"查看管理器命令目录", "加入用户 PATH", "从用户 PATH 移除", "返回首页"}
	default:
		userAdopt := "接管已有用户服务"
		if runtime.GOOS == "windows" {
			userAdopt += "（Windows 不支持）"
		}
		return []string{"首次安装主程序", "更新主程序", "接管已有前台安装", userAdopt, "接管已有系统服务", "服务管理", "技能管理", "配置与预算", "下载源", "PATH 管理", "检查两个程序的最新版本", "更新 vasm 自身", "诊断", "卸载主程序并保留数据", "退出"}
	}
}

// pageTitle returns a short title for one management page.
// pageTitle 返回一个管理页面的简短标题。
func pageTitle(page string) string {
	switch page {
	case "service":
		return "服务管理"
	case "skills":
		return "技能管理"
	case "config":
		return "配置与工具结果预算"
	case "source":
		return "下载源"
	case "path":
		return "PATH 管理"
	default:
		return "首页"
	}
}

// selectMenu maps a visible menu choice to its equivalent CLI operation.
// selectMenu 将可见菜单选项映射到等价命令行操作。
func (m model) selectMenu() (tea.Model, tea.Cmd) {
	switch m.page {
	case "home":
		switch m.cursor {
		case 0:
			m.page, m.cursor = "wizard", 0
		case 1:
			return m.execute([]string{"update"})
		case 2, 3, 4:
			if m.cursor == 3 && runtime.GOOS == "windows" {
				m.page, m.result, m.errorText = "result", "", "Windows 不支持用户作用域的本机服务"
				m.resultOffset = 0
				return m, nil
			}
			m.beginInput("已有完整发布包的运行根目录", []string{"__adopt", []string{"foreground", "user", "system"}[m.cursor-2]}, "")
		case 5, 6, 7, 8, 9:
			m.page, m.cursor = []string{"service", "skills", "config", "source", "path"}[m.cursor-5], 0
		case 10:
			return m.execute([]string{"check-updates"})
		case 11:
			return m.execute([]string{"update-self"})
		case 12:
			return m.execute([]string{"doctor", "--json"})
		case 13:
			m.page = "confirm"
			m.pendingCommand = []string{"uninstall"}
			m.pendingLabel = "将卸载主程序，保留配置、技能、状态和日志。"
			return m, nil
		default:
			m.cancel()
			return m, tea.Quit
		}
	case "service":
		commands := [][]string{{"service", "status"}, {"start"}, {"stop"}, {"restart"}, {"service", "install"}, {"service", "startup", "auto"}, {"service", "startup", "manual"}, {"service", "uninstall"}}
		if m.cursor < len(commands) {
			return m.execute(commands[m.cursor])
		}
		m.page, m.cursor = "home", 0
	case "skills":
		switch m.cursor {
		case 0:
			return m.execute([]string{"skills", "list", "--layer", "ROOT"})
		case 1:
			m.beginInput("ROOT 系统技能 GitHub 地址", []string{"__root_install"}, "")
		case 2:
			return m.execute([]string{"skills", "update", "--layer", "ROOT"})
		case 3:
			return m.execute([]string{"skills", "list", "--layer", "USER"})
		case 4, 5, 6:
			action := []string{"install", "update", "uninstall"}[m.cursor-4]
			m.beginInput("USER 技能 GitHub 地址或技能 ID", []string{"skills", action}, "")
		case 7:
			m.page, m.cursor = "home", 0
		}
	case "config":
		switch m.cursor {
		case 0:
			return m.execute([]string{"config", "show", "--json"})
		case 1, 2:
			return m.execute([]string{"config", "set", "vmm_enable", []string{"true", "false"}[m.cursor-1]})
		case 3:
			m.beginInput("VMM HTTP/HTTPS 地址", []string{"config", "set", "vmm"}, "http://127.0.0.1:17625")
		case 4:
			m.beginInput("默认工具结果字节上限", []string{"config", "budget", "default"}, "20000")
		case 5:
			m.beginInput("请求头 Vulcan-Client-Match-Name 的精确值", []string{"__budget_pattern"}, "")
		case 6, 7:
			return m.execute([]string{"config", "skill", "@auto-install", []string{"true", "false"}[m.cursor-6]})
		case 8, 9:
			m.beginInput("已安装系统技能的精确名称", []string{"__skill_toggle", []string{"true", "false"}[m.cursor-8]}, "")
		case 10:
			m.page, m.cursor = "home", 0
		}
	case "source":
		switch m.cursor {
		case 0:
			return m.execute([]string{"source", "show"})
		case 1:
			return m.execute([]string{"source", "set", "github"})
		case 2:
			return m.execute([]string{"source", "set", "mirror"})
		case 3:
			m.beginInput("自定义 HTTPS 代理基址", []string{"source", "set", "mirror"}, "https://")
		case 4:
			m.page, m.cursor = "home", 0
		}
	case "path":
		commands := [][]string{{"path", "show"}, {"path", "add"}, {"path", "remove"}}
		if m.cursor < len(commands) {
			return m.execute(commands[m.cursor])
		}
		m.page, m.cursor = "home", 0
	}
	return m, nil
}

// wizardRows presents download choices first, then configuration choices after verification.
// wizardRows 先展示下载选项，完成校验后再展示安装配置选项。
func (m model) wizardRows() []string {
	sources := []string{"GitHub 官方", "国内代理预设", "自定义 HTTPS 代理"}
	versions := []string{"最新正式版", "指定标签: " + m.tag}
	if m.preparedTag == "" {
		return []string{
			"下载源: " + sources[m.source],
			"自定义镜像: " + m.mirror,
			"主程序版本: " + versions[m.version],
			"下载并校验主程序包，然后配置安装",
		}
	}
	modes := []string{"前台命令行", "用户服务", "系统服务"}
	if runtime.GOOS == "windows" {
		modes[1] = "用户服务（Windows 不支持）"
	}
	root := "默认用户目录"
	if m.root != "" {
		root = m.root
	}
	vmmURL := "使用主程序默认地址"
	if m.vmmURL != "" {
		vmmURL = m.vmmURL
	}
	return []string{
		fmt.Sprintf("对接 VMM: %t", m.vmm),
		"VMM 地址: " + vmmURL,
		"运行方式: " + modes[m.mode],
		fmt.Sprintf("服务开机自启: %t", m.autostart),
		fmt.Sprintf("立即安装已启用系统技能: %t", m.initSkills),
		"系统技能选择（default/none/逗号分隔名称）: " + m.skillNames,
		fmt.Sprintf("加入用户 PATH: %t", m.addPath),
		"安装目录: " + root,
		"安装已校验的主程序包",
	}
}

// updateWizard downloads the selected Release before editing and committing install settings.
// updateWizard 先下载选定发布包，再编辑并提交安装设置。
func (m model) updateWizard(key string) (tea.Model, tea.Cmd) {
	rows := m.wizardRows()
	if key == "up" || key == "k" {
		m.cursor = (m.cursor + len(rows) - 1) % len(rows)
		return m, nil
	}
	if key == "down" || key == "j" {
		m.cursor = (m.cursor + 1) % len(rows)
		return m, nil
	}
	if key != "enter" && key != " " && key != "space" {
		return m, nil
	}
	if m.preparedTag == "" {
		switch m.cursor {
		case 0:
			m.source = (m.source + 1) % 3
		case 1:
			m.beginInput("自定义 HTTPS 代理基址", nil, m.mirror)
		case 2:
			m.version = (m.version + 1) % 2
			if m.version == 1 && m.tag == "" {
				m.beginInput("主程序标签，例如 v0.1.0", nil, "v0.1.0")
			}
		case 3:
			if m.source == 2 && m.mirror == "" {
				m.beginInput("自定义 HTTPS 代理基址", nil, "https://")
				return m, nil
			}
			if m.version == 1 && m.tag == "" {
				m.beginInput("主程序标签，例如 v0.1.0", nil, "v0.1.0")
				return m, nil
			}
			command := []string{"--internal-prepare-install"}
			if m.source > 0 {
				command = append(command, "--source", "mirror")
				if m.source == 2 {
					command = append(command, "--mirror-base", m.mirror)
				}
			}
			if m.version == 1 {
				command = append(command, "--app-version", m.tag)
			}
			m.pendingPrepare = true
			return m.execute(command)
		}
		return m, nil
	}
	switch m.cursor {
	case 0:
		m.vmm = !m.vmm
	case 1:
		m.beginInput("VMM HTTP/HTTPS 地址；留空使用主程序默认地址", nil, m.vmmURL)
	case 2:
		if runtime.GOOS == "windows" {
			m.mode = 2 - m.mode
		} else {
			m.mode = (m.mode + 1) % 3
		}
	case 3:
		m.autostart = !m.autostart
	case 4:
		m.initSkills = !m.initSkills
	case 5:
		m.beginInput("系统技能名称：default、none 或逗号分隔名称；安装时按发布包核对", nil, m.skillNames)
	case 6:
		m.addPath = !m.addPath
	case 7:
		m.beginInput("安装目录绝对路径；留空使用默认目录", nil, m.root)
	case 8:
		command := []string{"install", "--yes", "--app-version", m.preparedTag, "--prepared-archive", m.preparedArchive}
		if m.source > 0 {
			command = append(command, "--source", "mirror")
			if m.source == 2 {
				command = append(command, "--mirror-base", m.mirror)
			}
		}
		if m.root != "" {
			command = append(command, "--runtime-root", m.root)
		}
		command = append(command, "--vmm", fmt.Sprintf("%t", m.vmm))
		if m.vmmURL != "" {
			command = append(command, "--vmm-url", m.vmmURL)
		}
		if m.initSkills {
			command = append(command, "--init-skills")
		}
		if m.skillNames != "default" {
			command = append(command, "--skills", m.skillNames)
		}
		if m.addPath {
			command = append(command, "--add-path")
		}
		if m.mode > 0 {
			command = append(command, "--service")
			if m.mode == 1 {
				command = append(command, "--scope", "user")
			} else {
				command = append(command, "--scope", "system")
			}
			if m.autostart {
				command = append(command, "--startup", "auto")
			}
		}
		mode := "前台命令"
		if m.mode == 1 {
			mode = "用户服务"
		} else if m.mode == 2 {
			mode = "系统服务"
		}
		root := m.root
		if root == "" {
			root = "默认用户目录"
		}
		source := "GitHub 官方"
		if m.source == 1 {
			source = "国内代理预设"
		} else if m.source == 2 {
			source = m.mirror
		}
		m.pendingInstall = true
		m.pendingCommand = command
		m.pendingLabel = fmt.Sprintf("即将安装 %s (%s)\n下载源: %s\n安装目录: %s\n运行方式: %s；开机自启: %t\nVMM: %t；系统技能: %s；立即初始化: %t\n加入 PATH: %t", m.preparedTag, m.preparedAsset, source, root, mode, m.autostart, m.vmm, m.skillNames, m.initSkills, m.addPath)
		m.page = "confirm"
		return m, nil
	}
	return m, nil
}

// beginInput opens a small text editor for a wizard field or management command.
// beginInput 打开用于向导字段或管理命令的小型文本编辑器。
func (m *model) beginInput(label string, command []string, initial string) {
	m.page = "input"
	m.inputLabel = label
	m.inputCommand = command
	m.inputText = initial
}

// updateInput edits printable text and applies the confirmed value.
// updateInput 编辑可打印文本并应用已确认的输入值。
func (m model) updateInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.page = "wizard"
		if len(m.inputCommand) > 0 {
			m.page = "home"
		}
		return m, nil
	case "backspace":
		characters := []rune(m.inputText)
		if len(characters) > 0 {
			m.inputText = string(characters[:len(characters)-1])
		}
		return m, nil
	case "enter":
		if len(m.inputCommand) > 0 {
			if len(m.inputCommand) == 3 && m.inputCommand[0] == "source" && m.inputCommand[1] == "set" && m.inputCommand[2] == "mirror" && strings.TrimSpace(m.inputText) == "" {
				return m, nil
			}
			if m.inputCommand[0] == "__adopt" {
				command := []string{"adopt", "--runtime-root", m.inputText}
				if m.inputCommand[1] != "foreground" {
					command = append(command, "--service-installed", "--scope", m.inputCommand[1])
				}
				return m.execute(command)
			}
			if m.inputCommand[0] == "__root_install" {
				return m.execute([]string{"skills", "install", m.inputText, "--layer", "ROOT"})
			}
			if m.inputCommand[0] == "__budget_pattern" {
				m.beginInput("该客户端的工具结果字节上限", []string{"config", "budget", m.inputText}, "20000")
				return m, nil
			}
			if m.inputCommand[0] == "__skill_toggle" {
				return m.execute([]string{"config", "skill", m.inputText, m.inputCommand[1]})
			}
			return m.execute(append(append([]string{}, m.inputCommand...), m.inputText))
		}
		switch m.cursor {
		case 1:
			if m.preparedTag == "" {
				m.mirror = m.inputText
			} else {
				m.vmmURL = m.inputText
			}
		case 2:
			m.tag = m.inputText
		case 5:
			m.skillNames = m.inputText
		case 7:
			m.root = m.inputText
		}
		m.page = "wizard"
		return m, nil
	default:
		if key.Key().Text != "" {
			m.inputText += key.Key().Text
		}
		return m, nil
	}
}

// updateResult scrolls a completed operation and returns to the home page.
// updateResult 滚动查看已完成操作并返回首页。
func (m model) updateResult(key string) (tea.Model, tea.Cmd) {
	lines := strings.Split(m.result, "\n")
	switch key {
	case "up", "k":
		if m.resultOffset > 0 {
			m.resultOffset--
		}
	case "down", "j":
		if m.resultOffset < len(lines)-1 {
			m.resultOffset++
		}
	case "enter", "esc", "q":
		m.page, m.cursor = "home", 0
	}
	return m, nil
}

// execute starts one equivalent CLI command without blocking terminal redraws.
// execute 启动一条等价命令行命令，而不会阻塞终端重绘。
func (m model) execute(command []string) (tea.Model, tea.Cmd) {
	m.page = "working"
	m.progress = "准备中"
	m.events = make(chan tea.Msg, 64)
	go func() {
		writer := &eventWriter{events: m.events}
		err := m.runner(m.ctx, command, writer)
		m.events <- operationResult{output: writer.contents.String(), err: err}
	}()
	return m, waitEvent(m.events)
}

// waitEvent returns the next progress or completion message from one operation.
// waitEvent 返回一项操作的下一条进度或完成消息。
func waitEvent(events chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

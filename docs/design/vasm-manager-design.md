# Vulcan Agent Service Manager 完整功能设计

状态：设计稿；编写日期：2026-09-23。本文描述目标产品和分阶段交付边界，不表示相关接口已经实现。

## 1. 目标与边界

`vasm` 是一个独立可执行程序，首次运行负责安装 Vulcan Agent Service，安装后继续提供 TUI 与命令行管理入口。Windows、Linux x64、Linux ARM64、macOS Intel、macOS ARM64 均独立编译。`vasm` 不链接 LuaSkills、VMM 或主程序的原生运行库，也不随主程序完整包分发。

两个仓库独立维护版本与 Release：

| 仓库 | 拥有内容 | 发布产物 |
| --- | --- | --- |
| `OpenVulcan/vulcan-agent-service-manager` | `vasm`、薄安装脚本、下载与安装状态、TUI、管理命令 | 五个平台的独立安装器包、校验文件、脚本 |
| `OpenVulcan/vulcan-agent-service` | 服务主程序、LuaSkills 运行资源、控制器、配置模板、原生服务实现 | 五个平台的完整程序包、校验文件、应用清单 |

管理器通过公开的发布产物格式和子进程命令与主程序交互，不复制主程序的服务管理、技能安装或运行时实现。管理器版本与主程序版本不要求相同；安装前检查兼容契约版本。

以下范围均属于产品目标：首次安装、已安装实例管理、应用升级与回滚、管理器自身升级、卸载、下载源选择、VMM 可选接入、原生服务或前台命令模式、PATH、系统和用户技能管理、客户端预算与配置查看。AI memory 不加入默认技能，也不重新引入对应配置。

## 2. 已核实的主程序能力

本节以 `vulcan-agent-service` 的 `be74e74aaf2e07a01e6090e8c24d935a97b4e74b` 为基线。实施前必须重新检查目标版本。

| 能力 | 当前事实 | 管理器处理方式 |
| --- | --- | --- |
| 完整包 | 五个平台归档，内含 `bin/`、`configs/`、`lua_runtime/`、`release-manifest.json`；每包有 `.sha256` | 下载后核对摘要、归档路径、清单标签与平台 |
| 服务命令 | `service install/uninstall/start/stop/restart/status/print-definition`；安装可指定 `--runtime-root`、`--scope`、`--startup auto\|manual`、`--start`、`--force` | 使用现有命令执行已支持的动作，不解析其平台文本输出作为长期协议 |
| 系统技能 | `init`、`--install-root-skill`、`--update-root-skills`；`system_skills.json` 含 `auto_install` 和每项 `enabled` | 初装先生成选择清单，再调用主程序初始化；主动禁用保持禁用 |
| 用户技能 | 宿主 `skill-manager` 工具固定作用于 USER 层，支持列表、安装、更新、卸载；当前 URL 安装未实现 | TUI 单独区分 ROOT 与 USER；不把 USER 操作误投 ROOT |
| VMM | `config.yaml` 中 `vmm_enable` 与 `vmm` 控制可选连接 | 默认关闭，启用时要求端点并做实际连接诊断 |
| 客户端预算 | `Vulcan-Client-Match-Name` 请求头和 `VULCAN_CLIENT_MATCH_NAME` 环境变量可覆盖匹配名；`client_budgets.yaml` 定义工具结果等预算 | 显示真实配置语义；不把预算字节数称为模型上下文窗口 |

当前 `service status` 在 Windows 输出 `sc.exe` 文本，Linux 输出 systemd 文本，macOS 输出 launchd 文本；没有统一 JSON 状态。管理器对已核实的原生输出做受限解析，并直接调用对应原生服务管理器修改自启策略；无法判定状态时显式报错。未来统一管理契约见第 10 节。

## 3. 用户旅程与 TUI 信息架构

启动方式：`vasm` 打开 TUI；`vasm <command>` 运行相同操作的非交互命令。TUI 必须检测交互终端；没有终端时给出明确错误与对应 CLI 用法。退出、异常和中断都必须恢复终端状态。

首次运行：

1. 检测平台、架构、权限和既有安装记录；如用户指定已有运行根，先校验而不是搜索或猜测目录。
2. 选择 GitHub 官方或已配置的国内镜像、稳定版或指定标签，并展示要下载的仓库、标签、文件名、大小和来源域名。
3. 选择用户级或系统级安装位置；下载并校验主程序完整包，在临时目录解压和检查 `release-manifest.json`。
4. 选择前台命令模式或平台原生服务模式。服务模式提供名称、作用域、自启、安装后启动选项；Windows 系统服务动作需要提权。
5. 配置 VMM 开关与端点；选择默认系统技能及自动安装策略；决定是否将 `vasm` 加入 PATH。
6. 显示所有将写入的文件和系统变更，确认后提交安装，调用主程序 `init`、按选择安装/启动服务并执行健康检查。
7. 展示结果、失败项、日志路径及可重试动作。若技能下载失败，不误报整个程序包尚未安装。

已安装首页显示管理器版本、主程序标签、运行根、来源、运行模式、服务状态、VMM 状态、系统技能状态和最近一次操作结果。一级页面为“安装与更新”“服务”“技能”“配置与客户端预算”“下载源与镜像”“诊断与日志”“卸载”。每一项展示当前值和待应用值；保存前提供变更预览。

终端界面采用固定的三段信息结构：顶部显示当前实例与健康状态，中部显示页面表单或操作进度，底部显示快捷键、当前阶段与错误摘要。首页示意如下；窄终端改为单列并允许滚动，不裁掉确认按钮或错误正文。

```text
┌ vasm 0.x  ·  Agent Service v0.x  ·  运行根 ─────────────────────┐
│ 来源: GitHub / 镜像    模式: 前台 / 原生服务    状态: 运行 / 停止 │
├──────────────────────────────────────────────────────────────┤
│ 安装与更新   服务   技能   配置   下载源   诊断   卸载           │
│                                                              │
│ 当前页面内容、修改前后对比、进度及逐项结果                     │
├──────────────────────────────────────────────────────────────┤
│ Enter 选择  Tab 切换  Esc 返回  ? 帮助  Ctrl+C 安全退出         │
└──────────────────────────────────────────────────────────────┘
```

确认页必须同时列出下载源域名、目标版本、安装路径、服务名称与作用域、PATH 变更和需要网络安装的技能。取消键仅取消当前可中断阶段；正在提交文件或服务定义时先完成安全点或执行回滚，再退出。`vasm run` 在交出终端给前台主程序前恢复常规终端模式，主程序退出后再恢复 TUI。

服务页面支持安装、卸载、启动、停止、重启、查看状态、自动/手动开机启动。前台命令模式提供 `vasm run`，将主程序运行在当前终端并透传退出码；不私自创建第二套后台守护进程。服务动作只针对登记的服务名与作用域。

技能页面把“系统 ROOT 技能”和“用户 USER 技能”分区。ROOT 使用系统清单与主程序的 ROOT 命令；USER 使用主程序的用户技能管理契约。更新操作必须展示技能 ID、原始来源和结果，局部失败逐项显示。默认技能选择只控制初始化清单，不能覆盖用户之后的主动禁用状态。

## 4. 命令行表面

以下是 `vasm` 的目标命令，TUI 和 CLI 调用同一应用服务层；`--json` 输出稳定的机器可读结果。命令中的 `<root>` 只能来自显式参数或管理器安装记录。`vasm install` 在缺少完整选项时进入安装向导；无交互安装必须提供经过校验的选项文件与 `--yes`，不能靠默认值猜测 VMM、服务和技能选择。

```text
vasm                                      # 打开 TUI
vasm install [--source github|mirror] [--mirror-base URL] [--app-version TAG]
vasm install --options-file <file.json> --yes
vasm adopt --runtime-root <root>          # 显式接管已安装实例
vasm status [--json]
vasm run                                  # 前台运行；服务模式下不执行
vasm start | stop | restart
vasm service install | uninstall | status
vasm service startup auto | manual
vasm skills list [--layer ROOT|USER]
vasm skills install <source> [--layer ROOT|USER]
vasm skills update [<skill-id>] [--layer ROOT|USER]
vasm skills uninstall <skill-id> --layer USER
vasm config show [--json]
vasm config edit                          # 打开 TUI 配置页
vasm source show | set github | set mirror <base-url>
vasm update [--app-version TAG]
vasm update-self [--manager-version TAG]
vasm doctor [--json]
vasm uninstall [--keep-data]
```

ROOT 卸载不暴露为普通快捷操作，避免移除宿主所依赖的系统技能；可以通过系统技能清单主动禁用。当前主程序不提供全部命令的 JSON 契约，`vasm` 不应靠解析人类可读文本来模拟已完成的能力。

## 5. 安装脚本与独立管理器发布

仓库提供 `scripts/install.ps1` 和 `scripts/install.sh`。脚本仅做平台识别、来源解析、下载管理器包及对应校验文件、SHA-256 校验、解压到临时目录、启动 `vasm` 并传递来源/版本参数；不下载主程序、不写主程序配置、不安装服务。脚本自身通过 HTTPS 分发。

PowerShell 的 `irm ... | iex` 场景通过 `VASM_SOURCE`、`VASM_MIRROR_BASE`、`VASM_VERSION` 环境变量传入初始选项；脚本不得依赖 `$PSScriptRoot`。Unix 的 `curl -fsSL ... | sh -s -- --source mirror --mirror-base ...` 使用命令参数；启动 TUI 时从终端设备读取交互输入，不能把脚本管道当成交互终端。Windows 使用 `Get-FileHash`，Linux 使用可用的 SHA-256 工具，macOS 使用 `shasum -a 256`。缺少校验工具时停止，不能跳过校验。

管理器五个平台各发布固定资产名，标签负责版本隔离：

| 平台 | 管理器资产 |
| --- | --- |
| Windows x64 | `vasm-windows-x64.zip` |
| Linux x64 | `vasm-linux-x64.tar.gz` |
| Linux ARM64 | `vasm-linux-arm64.tar.gz` |
| macOS Intel | `vasm-macos-x64.tar.gz` |
| macOS ARM64 | `vasm-macos-arm64.tar.gz` |

每个资产配同名 `.sha256`，另发布 `SHA256SUMS`。固定资产名使脚本可使用 GitHub 官方的 `releases/latest/download/<asset>` 地址；指定标签则使用 `releases/download/<tag>/<asset>`。Windows 包必须自带运行所需组件，或以经验证的独立运行方式编译；不允许依赖主程序包内的 CRT。Unix 包必须保留可执行权限。发布工作流只由手动选择已有标签触发，在五个平台原生 runner 上分别编译、测试、解包冒烟、校验完整资产集合，再发布 Release。

管理器版本独立于主程序版本。`vasm update-self` 下载新包并校验；Windows 运行中可执行文件通过退出后的独立临时辅助进程替换，成功后重新打开；失败保留旧管理器。Linux/macOS 使用同目录暂存和原子替换。自更新不能在一半状态下删除现有管理器。

## 6. 下载源与镜像契约

下载源是显式选择的配置，不做无提示的跨源回退。失败时展示当前源与错误，并允许用户主动切换。官方源固定为 OpenVulcan 的两个 GitHub 仓库；国内代理预设为用户主动选择的 `https://gh-proxy.com`，也支持自定义 HTTPS 代理基址。

镜像只负责转发归档资产。管理器先从 GitHub 官方 Release API 取得标签、文件名、大小和 SHA-256 摘要，再按以下代理前缀构造传输地址：

```text
<base>/https://github.com/OpenVulcan/<repo>/releases/download/<tag>/<asset>
```

`<repo>` 只允许 `vulcan-agent-service-manager` 或 `vulcan-agent-service`。最新版由 GitHub 官方 API 返回的发布标签确定，不依赖镜像的 `latest` 指向。管理器在独立偏好文件中保存来源类型与基址，主程序更新沿用已选来源。所有跳转仅接受 HTTPS；下载限制大小、超时并校验完整摘要。手动输入标签时只取该标签的精确资产，不轮询候选文件名。

主程序当前资产名包含版本号，而管理器资产名固定。GitHub 官方 Release API 对两者都提供最新版标签和资产摘要，因此 v0.1.0 主程序也可以通过镜像安装最新版。官方元数据不可达或缺少摘要时拒绝安装，不信任镜像同源的 `.sha256`，也不猜测资产文件名。

管理器自身发布流程生成以下信息索引，便于发布审计；安装和自更新的信任依据仍是官方 GitHub API 元数据：

```json
{
  "schema_version": 1,
  "repository": "OpenVulcan/vulcan-agent-service-manager",
  "tag": "v0.1.0",
  "assets": [
    {
      "platform": "windows-x64",
      "name": "vasm-windows-x64.zip",
      "size_bytes": 0,
      "sha256": "由发布流程填入实际摘要"
    }
  ]
}
```

实际发布时 `assets` 必须恰好覆盖五个平台；示例中的 `0` 和文字只表示字段类型与归属，不是合法发布值。该索引不承担验签职责，也不替代官方 API 返回的摘要。

现有 `.sha256` 与 `SHA256SUMS` 可供发布者和引导脚本核对。管理器安装主程序时从 `api.github.com` 的 HTTPS 响应取得资产摘要，并以此校验官方或镜像传输的归档。薄脚本也只从 GitHub 官方发布地址取得校验文件；官方端点不可达时停止，不接受镜像提供的校验值。首版不管理独立签名私钥。

技能包安装是另一条下载链：当前系统清单为 GitHub 仓库定位值，USER 工具也使用上游 LuaSkills 的安装来源。切换主程序资产镜像不会自动改变技能下载来源。TUI 必须清楚显示这一区别；若以后要求技能也走国内镜像，应先由 LuaSkills/主程序定义受支持的包源协议，再接入管理器。

## 7. 安装布局、事务与升级

管理器的安装记录与主程序运行根分开存放。薄脚本下载并校验管理器包后，将 `vasm` 放入稳定的用户命令目录并打开 TUI；临时包随后清理。用户级默认路径使用当前平台的标准用户数据目录，PATH 只加入稳定的 `vasm` 入口位置。系统服务操作需要已具备相应权限的终端。

主程序保持当前发布布局：`<runtime-root>/bin`、`<runtime-root>/configs`、`<runtime-root>/lua_runtime`。主程序以显式 `--runtime-root` 接入，服务注册的可执行文件始终位于稳定的 `<runtime-root>/bin`。服务安装记录保存格式版本、运行根、主程序标签、上次安装来源、服务名/作用域及自启策略；管理器偏好文件单独保存当前下载源和 PATH 所有权。接管既有安装必须由用户提供运行根，验证清单与布局后才写服务安装记录。

安装/升级事务状态为“预检 → 下载 → 校验 → 暂存 → 配置预览 → 停止旧服务 → 提交 → 初始化/启动 → 健康检查 → 完成”。下载与校验失败不触碰已安装目录。解包拒绝绝对路径、`..`、越界符号链接、重复目标路径和非预期文件类型；使用进程锁阻止两个 `vasm` 同时修改同一实例。

升级保留 `configs/` 下的用户配置，以及 `lua_runtime` 的技能、状态、数据库、用户数据、受管环境等可写内容；只替换发布包拥有的 `bin/`、静态库、Lua 包、共享资源、运行时发行包和控制器。替换前做同盘备份，停止并确认旧服务进程退出；提交后校验主程序启动、服务状态和配置。失败时恢复旧文件及旧服务状态，并保留诊断日志。未知的用户文件不归入自动删除列表。Windows 文件锁场景必须等服务真正停止后再替换。

新模板与旧配置分开保存；已有 `config.yaml`、`system_skills.json`、`client_budgets.yaml` 不被静默覆盖。配置格式升级由主程序提供校验/迁移契约，管理器展示差异并备份后应用。卸载分为“仅卸载服务”“卸载程序但保留配置与技能数据”“彻底卸载”；最后一种必须逐项展示将删除的精确路径。PATH 撤销只处理管理器自己添加的条目。

## 8. 配置页与真实字段映射

| TUI 项 | 当前持久化位置或命令 | 生效方式 |
| --- | --- | --- |
| VMM 启用与端点 | `configs/config.yaml` 的 `vmm_enable`、`vmm` | 修改后重启主程序，并通过宿主实际状态验证 |
| 默认系统技能与自动安装 | `configs/system_skills.json` 的 `auto_install`、`skills[].enabled` | 执行 `init` 或在下次启动时按策略安装缺失项；主动禁用优先 |
| 服务名、作用域、自启 | 主程序原生服务命令与管理器安装记录 | 调用主程序服务契约并读取实际平台状态 |
| HTTP/gRPC 监听地址 | `configs/config.yaml` 的 `http`、`grpc` | 校验端口后重启主程序 |
| 客户端识别请求头 | 固定头 `Vulcan-Client-Match-Name`；固定环境变量 `VULCAN_CLIENT_MATCH_NAME` | 由请求发送方设置，管理器不能替客户端注入请求头 |
| 客户端工具结果预算 | `configs/client_budgets.yaml` 的 `clients`、`grpc_clients`、预算度量 | 主程序的运行配置重载或重启后生效 |
| 当前配置 | 主程序配置文件、系统技能文件、管理器记录及实际服务状态 | 展示来源、有效值与脱敏信息 |

用户已确认“固定上下文长度”指工具结果上限。界面将 `Vulcan-Client-Match-Name` 的精确值映射到 `client_budgets.yaml` 中对应客户端的 `tool_result.bytes.default`，同时支持修改全局默认字节上限。该设置不更改模型上下文窗口。

配置修改不能靠字符串替换或重写整份 YAML 来破坏用户注释和未知字段。初装从发行模板生成配置；已安装实例仅修改已确认归属的字段，并保留未知节点和注释。修改采用同目录临时文件提交，生效时机按主程序当前能力提示。

## 9. 权限、运行状态与错误处理

默认推荐用户级目录与前台命令模式。用户主动选择系统服务时，Windows 使用 SCM，Linux 使用 systemd，macOS 使用 launchd；Linux/macOS 允许平台实际支持的用户或系统作用域。提权仅覆盖需系统权限的服务与路径操作。服务名、作用域和运行根由管理器记录确定，不从人类可读 `status` 文本反推。

所有后台下载、校验和安装阶段都要显示进度、当前文件、已传输量与取消结果。取消在提交前删除暂存；提交后取消走明确回滚。网络错误显示来源 URL、HTTP 状态或传输错误、可重试阶段，不把 404 当成其他平台候选文件的信号。技能安装可以部分成功，但必须逐项记录成功和失败并返回非零总体状态。

诊断页检查运行根清单、配置格式、主程序可执行性、服务管理器状态、VMM 启用状态、PATH 解析、下载源连通性与最近一次事务。诊断命令只读；日志包含阶段、错误码和路径，不记录密钥或请求头敏感值。

## 10. 已核实的主程序契约与未来扩展

主程序 v0.1.0 已提供版本化 `release-manifest.json`、`init`、原生服务安装和生命周期命令、ROOT 技能安装及更新、USER `skill-manager` 工具调用，以及配置文件中的 VMM、系统技能和客户端工具结果预算字段。管理器依据这些已存在的路径和参数实现首版功能，避免在主程序仓库新增重复接口。

服务状态目前由主程序转发 Windows `sc.exe`、Linux `systemctl` 或 macOS `launchctl` 文本。管理器只解释源码中已确认的状态值，其他状态明确报错；服务自启调用相同的原生服务管理器。配置只编辑已确认字段。若未来需要统一 JSON 状态、能力握手或热重载结果，可另行设计版本化管理契约；首版不宣称这些接口已存在。

## 11. 技术结构与发布流程

### 11.1 语言选型：Go

本仓库是独立程序；它通过发布清单、文件和子进程命令与 Rust 主程序交互，没有共享 Rust 类型、ABI 或需要复用的主程序 crate。因此没有“主程序用 Rust，安装器也必须用 Rust”的技术约束。

| 维度 | Go | Rust |
| --- | --- | --- |
| 终端界面 | Bubble Tea v2 与 Bubbles 提供状态更新、表单和进度组件，适合向导与管理页 | Ratatui + Crossterm 同样成熟，布局和绘制控制更细 |
| 下载、归档、摘要 | `net/http`、`archive/zip`、`archive/tar`、`crypto/sha256` 均由标准库提供 | 也能可靠实现，但通常需要分别引入第三方 crate |
| 五平台构建 | 保持纯 Go 且 `CGO_ENABLED=0` 时可用 `GOOS/GOARCH` 编译目标二进制，不要求安装主程序的 C 运行库 | 同样能覆盖目标平台；原生依赖与 Windows CRT 的打包需要额外核对 |
| 运行开销 | 单文件分发方便，但包含 Go 运行时与垃圾回收；实际体积和内存需测量 | 通常更便于控制二进制和运行时开销，具体数值也需测量 |
| 与主程序交互 | 通过已核实的命令与配置文件交互，语言无影响 | 可共享 Rust 代码，但会让独立仓库重新耦合主程序内部实现；不建议以此为理由选 Rust |

`vasm` 采用纯 Go 单程序，TUI 采用 Bubble Tea v2；下载、校验和归档使用标准库。`CGO_ENABLED=0` 是发布约束，依赖加入时必须验证其确实能在五个平台无 cgo 编译。Rust 仍是可行备选，尤其当实测表明 Go 产物体积或常驻内存不满足发布目标时；不能在没有测量的情况下声称某一语言一定更小或更快。

用户已授权进入实现。模块按终端界面、命令解析、发布源、下载校验、归档验证、安装事务、安装记录、主程序命令适配、平台集成和诊断划分。事件循环与长时间下载任务分离，TUI 和 CLI 共用同一应用服务层。跨平台编译只能证明产物可构建，五个平台的解包、终端和服务行为仍需原生验证。

### 11.2 发布流程

管理器仓库的工作流与主程序仓库解耦：手动输入已有管理器标签，按五个平台矩阵执行静态检查、单元测试、原生编译和解包运行测试；聚合校验全部资产及 SHA-256；发布完成后从 GitHub 官方 URL 实际下载并运行 `vasm --version`。标签推送本身不触发发布。第三方镜像的可用性另行验证，不控制其同步时机。

验证矩阵至少覆盖：官方源与镜像源、固定标签与最新版、五个平台解包、损坏摘要、错误平台清单、路径穿越、断网与取消、首次安装、接管既有安装、服务/前台两模式、系统/用户作用域、VMM 开关、系统技能自动安装关闭、升级保留数据、失败回滚、PATH 添加/撤销、管理器自更新。真实服务启停测试必须在对应平台的原生环境执行，交叉编译不算通过。

## 12. 交付顺序与验收

1. **发布契约**：核实主程序 v0.1.0 的清单、命令及配置字段，并固定测试夹具。验收为管理器只使用有源码证据的能力。
2. **管理器骨架与下载源**：独立 `vasm`、TUI 框架、CLI、两类来源、脚本和五平台独立发布。验收为脚本只下载管理器、镜像可选、五平台产物均可单独运行。
3. **首次安装**：下载完整主程序、校验、解包、配置、`init`、服务或前台模式、PATH、安装记录。验收为新机器可从脚本完成安装，配置和实际状态一致。
4. **持续管理**：服务状态与自启、技能、配置、诊断、接管现有安装。验收为所有 TUI 动作与等价 CLI 命令具有同一结果和退出码。
5. **升级与恢复**：应用升级、回滚、自更新和卸载。验收为配置/技能/数据库不丢失，失败可回到升级前可运行状态。

阶段完成条件是每项功能均由其真实平台或服务端能力验证；仅有界面控件、命令拼接或跨平台编译不能代替端到端验收。

## 13. 已确认的产品决定

1. 国内下载提供 `https://gh-proxy.com` 作为可选预设，同时允许用户填写自定义 HTTPS 代理。代理的可用性以实时请求为准。
2. “固定上下文长度”指工具结果上限，按主程序现有 `client_budgets.yaml` 字节预算配置。
3. 首版采用 GitHub 官方 Release API 的资产摘要校验，不引入独立数字签名密钥；镜像只传输归档。

## 参考依据

- [主程序 v0.1.0 发布脚本](https://github.com/OpenVulcan/vulcan-agent-service/blob/be74e74aaf2e07a01e6090e8c24d935a97b4e74b/scripts/release.py)
- [主程序服务命令](https://github.com/OpenVulcan/vulcan-agent-service/blob/be74e74aaf2e07a01e6090e8c24d935a97b4e74b/src/bootstrap/cli.rs)
- [主程序系统技能清单](https://github.com/OpenVulcan/vulcan-agent-service/blob/be74e74aaf2e07a01e6090e8c24d935a97b4e74b/configs/system_skills.json)
- [主程序客户端预算结构](https://github.com/OpenVulcan/vulcan-agent-service/blob/be74e74aaf2e07a01e6090e8c24d935a97b4e74b/src/config/client_budget/types.rs)
- [GitHub 最新版固定资产链接](https://docs.github.com/en/repositories/releasing-projects-on-github/linking-to-releases)
- [GitHub Release 资产摘要字段](https://docs.github.com/en/rest/releases/assets)
- [Go 目标平台与架构](https://go.dev/doc/install/source)
- [Go cgo 编译开关](https://go.dev/cmd/cgo/)
- [Bubble Tea v2 官方说明](https://github.com/charmbracelet/bubbletea/blob/main/README.md)
- [Ratatui 终端后端](https://www.ratatui.rs/concepts/backends/)

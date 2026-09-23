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

当前 `service status` 在 Windows 输出 `sc.exe` 文本，Linux 输出 systemd 文本，macOS 输出 launchd 文本；没有统一 JSON 状态。当前 `--startup` 只在安装服务时设置，也没有独立的自启策略修改命令。完整管理体验需要主程序补充机器可读管理契约，见第 10 节。

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

下载源是显式选择的配置，不做无提示的跨源回退。失败时展示当前源与错误，并允许用户主动切换。官方源固定为 OpenVulcan 的两个 GitHub 仓库；镜像源由用户或发行方提供 HTTPS 基址，不预设第三方代理。

镜像必须原样提供两个仓库的发布资产和校验文件，保留相同标签、资产名、字节内容及以下路径语义：

```text
<base>/<repo>/releases/download/<tag>/<asset>
<base>/<repo>/releases/latest/download/<asset>
```

`<repo>` 只允许 `vulcan-agent-service-manager` 或 `vulcan-agent-service`。镜像的 `latest` 必须指向已完整同步、通过校验的公开版本；不得在文件仍同步时切换。管理器持久化来源类型与基址，后续主程序更新默认沿用，用户可以在单次操作中覆盖。所有跳转仅接受 HTTPS；下载限制大小、超时并显示实际最终域名。手动输入标签时只构造该标签的确定资产路径，不轮询候选文件名。

主程序当前资产名包含版本号，而管理器资产名固定。为使镜像和官方都能稳定发现主程序最新版本，主程序仓库需在后续 Release 增加固定名 `release-index.json`，记录标签、平台资产名、大小、摘要、应用清单契约版本；管理器从 `releases/latest/download/release-index.json` 读取。当前 v0.1.0 尚无此索引，过渡期可通过 GitHub Release API 取得官方资产元数据；镜像安装 v0.1.0 仅允许用户指定标签并提供与官方独立核对的预期摘要，不能只信任镜像的同源 `.sha256`。不能用猜测的 `latest` 文件名下载主程序。

新增索引采用固定版本结构，以下仅为拟定的字段契约；`sha256` 与 `size_bytes` 由发布流水线从实际资产计算，不在源码中手填：

```json
{
  "schema_version": 1,
  "repository": "OpenVulcan/vulcan-agent-service",
  "tag": "v0.1.1",
  "management_contract_version": 1,
  "assets": [
    {
      "platform": "windows-x64",
      "name": "vulcan-agent-service-v0.1.1-windows-x64.zip",
      "size_bytes": 0,
      "sha256": "由发布流程填入实际摘要"
    }
  ]
}
```

实际发布时 `assets` 必须恰好覆盖五个平台；示例中的 `0` 和文字只表示字段类型与归属，不是合法发布值。索引原始字节附带 `release-index.json.sig`，采用固定公钥验证的发布签名；签名密钥轮换必须先由旧管理器版本携带新公钥，再让发布方改用新密钥。签名验证失败禁止安装和升级。

现有 `.sha256` 与 `SHA256SUMS` 能检测传输损坏，但如果资产和校验文件都由被篡改的镜像提供，不能单独证明发布者身份。目标发布链应由主程序仓库对索引或校验清单签名，`vasm` 内置发布公钥并在安装前验证。管理器的薄脚本本身仍以脚本来源域名为信任根；在签名引导链完成之前，国内镜像应优先使用 OpenVulcan 控制的域名，并在界面明确展示所选来源。不会把“摘要正确”宣称为“镜像可信”。

技能包安装是另一条下载链：当前系统清单为 GitHub 仓库定位值，USER 工具也使用上游 LuaSkills 的安装来源。切换主程序资产镜像不会自动改变技能下载来源。TUI 必须清楚显示这一区别；若以后要求技能也走国内镜像，应先由 LuaSkills/主程序定义受支持的包源协议，再接入管理器。

## 7. 安装布局、事务与升级

管理器的安装记录与主程序运行根分开存放。薄脚本启动的临时 `vasm` 在首次安装提交阶段复制到稳定的管理器目录，验证复制品可运行后才添加 PATH；临时包随后清理。用户级默认路径使用当前平台的标准用户数据目录，系统级安装位置由用户确认；PATH 只加入稳定的 `vasm` 入口位置。管理员动作在展示具体命令和目标后提权，不以默认管理员权限运行整个 TUI。

主程序保持当前发布布局：`<runtime-root>/bin`、`<runtime-root>/configs`、`<runtime-root>/lua_runtime`。主程序以显式 `--runtime-root` 接入，服务注册的可执行文件始终位于稳定的 `<runtime-root>/bin`。管理器记录文件保存格式版本、运行根、主程序标签、下载源、运行模式、服务名/作用域、自启策略、PATH 归属、上次成功事务；不保存 VMM 密钥或其他敏感配置。接管既有安装必须由用户提供运行根，验证清单与布局后才写管理器记录。

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

用户提出的“某个头信息转映射固定的上下文长度”目前不能直接落到已存在字段：现有头信息改变客户端匹配名，`client_budgets.yaml` 控制工具结果的 bytes/tokens/lines，`model_config.yaml` 只有单次生成的 `max_tokens`，没有模型上下文窗口长度字段。设计将“客户端识别 → 工具结果预算”作为已定义功能；“固定上下文长度”另列为待定产品契约，必须先确定它指模型上下文窗口、工具结果上限，还是客户端上报长度，才增加字段和 UI。绝不把这三者混为一个数值。

配置修改不能靠字符串替换或重写整份 YAML 来破坏用户注释和未知字段。初装从发行模板生成配置；已安装实例的受支持字段修改由主程序提供校验并原子提交的管理命令。管理器负责收集输入、预览、调用契约与展示结果。

## 9. 权限、运行状态与错误处理

默认推荐用户级目录与前台命令模式。用户主动选择系统服务时，Windows 使用 SCM，Linux 使用 systemd，macOS 使用 launchd；Linux/macOS 允许平台实际支持的用户或系统作用域。提权仅覆盖需系统权限的服务与路径操作。服务名、作用域和运行根由管理器记录确定，不从人类可读 `status` 文本反推。

所有后台下载、校验和安装阶段都要显示进度、当前文件、已传输量与取消结果。取消在提交前删除暂存；提交后取消走明确回滚。网络错误显示来源 URL、HTTP 状态或传输错误、可重试阶段，不把 404 当成其他平台候选文件的信号。技能安装可以部分成功，但必须逐项记录成功和失败并返回非零总体状态。

诊断页检查运行根清单、配置格式、主程序可执行性、服务管理器状态、VMM 启用状态、PATH 解析、下载源连通性与最近一次事务。诊断命令只读；日志包含阶段、错误码和路径，不记录密钥或请求头敏感值。

## 10. 主程序仓库需要补齐的跨仓库契约

以下为新增需求，不是 v0.1.0 已具备的命令。管理器须通过契约版本握手检测，不因缺失而猜测或静默兼容。

1. Release 侧发布固定名 `release-index.json` 及发布者可验证的签名；包含主程序标签、五个平台资产名/大小/摘要、清单格式与最低管理契约版本。管理器自身 Release 也发布相同意义的签名索引，以便安装后的自更新验证。
2. `manage capabilities --json` 返回稳定契约版本、应用版本、支持的管理动作；`manage status --runtime-root <root> --json` 返回实例布局、服务模式和配置有效性，不泄露密钥。
3. `service status --json` 与 `service set-startup --startup auto|manual --json`，统一 Windows SCM、systemd、launchd 的安装、运行和自启状态；保留现有人类可读命令。
4. 受限的 `manage config show/validate/apply --json`，只允许受支持的 VMM、监听地址、系统技能和客户端预算字段；支持预览、原子提交、保留未知字段及备份。`config.yaml` 变更应明确提示重启；可热重载的预算应返回真实重载结果。
5. `manage skills list/install/update/uninstall --layer ROOT|USER --json` 或等价稳定接口，内部继续使用现有 ROOT/USER 归属与 LuaSkills 安装实现；当前 USER URL 安装不支持时明确报告能力缺失。

主程序 v0.1.0 可用于验证下载、解包、配置模板、`init`、ROOT 安装与已有服务动作；统一状态、自启修改、已安装配置编辑和完整 USER 技能管理必须等上述契约发布后才在 TUI 标记为可用。管理器的兼容矩阵按契约能力判定，不根据标签大小猜测功能。

## 11. 技术结构与发布流程

### 11.1 语言选型：推荐 Go，待最终确认

本仓库是独立程序；它通过发布清单、文件和子进程命令与 Rust 主程序交互，没有共享 Rust 类型、ABI 或需要复用的主程序 crate。因此没有“主程序用 Rust，安装器也必须用 Rust”的技术约束。

| 维度 | Go | Rust |
| --- | --- | --- |
| 终端界面 | Bubble Tea v2 与 Bubbles 提供状态更新、表单和进度组件，适合向导与管理页 | Ratatui + Crossterm 同样成熟，布局和绘制控制更细 |
| 下载、归档、摘要、签名 | `net/http`、`archive/zip`、`archive/tar`、`crypto/sha256`、`crypto/ed25519` 均由标准库提供 | 也能可靠实现，但通常需要分别引入第三方 crate |
| 五平台构建 | 保持纯 Go 且 `CGO_ENABLED=0` 时可用 `GOOS/GOARCH` 编译目标二进制，不要求安装主程序的 C 运行库 | 同样能覆盖目标平台；原生依赖与 Windows CRT 的打包需要额外核对 |
| 运行开销 | 单文件分发方便，但包含 Go 运行时与垃圾回收；实际体积和内存需测量 | 通常更便于控制二进制和运行时开销，具体数值也需测量 |
| 与主程序交互 | 通过稳定 JSON 管理契约调用主程序，语言无影响 | 可共享 Rust 代码，但会让独立仓库重新耦合主程序内部实现；不建议以此为理由选 Rust |

推荐将 `vasm` 实现为纯 Go 单程序，TUI 采用 Bubble Tea v2，所需通用组件采用对应版本的 Bubbles；下载、校验、归档和签名优先使用标准库。该选择最契合独立安装器的网络、文件和终端职责。`CGO_ENABLED=0` 是目标约束，依赖加入时必须验证其确实能在五个平台无 cgo 编译。Rust 仍是可行备选，尤其当实测表明 Go 产物体积或常驻内存不满足发布目标时；不能在没有测量的情况下声称某一语言一定更小或更快。

这是核心技术选型，当前仅形成推荐，待用户确认后才创建 `go.mod` 或引入 TUI 依赖。无论最终语言如何，模块按终端界面、命令解析、发布源、下载校验、归档验证、安装事务、安装记录、主程序命令适配、平台集成和诊断划分。事件循环与长时间下载任务分离，界面从只读状态快照绘制，TUI 和 CLI 共用同一应用服务层。跨平台编译只能证明产物可构建，五个平台的解包、终端和服务行为仍需原生验证。

### 11.2 发布流程

管理器仓库的工作流与主程序仓库解耦：手动输入已有管理器标签，按五个平台矩阵执行静态检查、单元测试、原生编译、解包运行测试和脚本模拟下载测试；聚合校验全部资产及 SHA-256；发布完成后从 GitHub 官方 URL 实际下载并运行 `vasm --version` 与 `vasm doctor --json`。标签推送本身不触发发布。镜像同步另做独立流程，只有全部资产与摘要一致才更新其 `latest` 指针。

验证矩阵至少覆盖：官方源与镜像源、固定标签与最新版、五个平台解包、损坏摘要、错误平台清单、路径穿越、断网与取消、首次安装、接管既有安装、服务/前台两模式、系统/用户作用域、VMM 开关、系统技能自动安装关闭、升级保留数据、失败回滚、PATH 添加/撤销、管理器自更新。真实服务启停测试必须在对应平台的原生环境执行，交叉编译不算通过。

## 12. 交付顺序与验收

1. **发布契约**：在主程序仓库确定 `release-index.json`、签名方式和管理命令版本；形成固定测试夹具。验收为管理器能区分 v0.1.0 已有能力和新能力。
2. **管理器骨架与下载源**：独立 `vasm`、TUI 框架、CLI、两类来源、脚本和五平台独立发布。验收为脚本只下载管理器、镜像可选、五平台产物均可单独运行。
3. **首次安装**：下载完整主程序、校验、解包、配置、`init`、服务或前台模式、PATH、安装记录。验收为新机器可从脚本完成安装，配置和实际状态一致。
4. **持续管理**：服务状态与自启、技能、配置、诊断、接管现有安装。验收为所有 TUI 动作与等价 CLI 命令具有同一结果和退出码。
5. **升级与恢复**：应用升级、回滚、自更新和卸载。验收为配置/技能/数据库不丢失，失败可回到升级前可运行状态。

阶段完成条件是每项功能均由其真实平台或服务端能力验证；仅有界面控件、命令拼接或跨平台编译不能代替端到端验收。

## 13. 需要产品决定的两项输入

1. 国内镜像的正式 HTTPS 基址及运营方。方案已支持可配置镜像，但未指定实际站点；发布签名和引导脚本的信任链需随镜像运营方式确定。
2. “固定上下文长度”的精确定义和单位。现有代码只证实客户端匹配名与工具结果预算，没有对应的模型上下文窗口配置；确认后才能把它写入跨仓库契约与 TUI。

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

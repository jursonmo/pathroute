## ADDED Requirements

### Requirement: 使用边状态和动态 cost 构图
动态路由模式下，系统 SHALL 只将 `graph_edges.status == 1` 的边加入路由图。可用边存在动态快照时使用快照 cost；没有快照时使用 cost 1000。动态模式不得覆盖或直接使用静态 `graph_edges.cost` 作为有效动态 cost。

#### Scenario: 排除不可用边
- **WHEN** 一条边的 `status` 不等于 1
- **THEN** 该边不进入路由计算，即使它存在较小的动态或静态 cost

#### Scenario: 可用边使用动态 cost
- **WHEN** 一条 `status == 1` 的边存在 cost 为 42 的当前动态快照
- **THEN** 路由图使用 cost 42

#### Scenario: 可用边没有动态快照
- **WHEN** 一条 `status == 1` 的边尚无动态快照
- **THEN** 路由图使用 cost 1000，且该边仍参与计算

### Requirement: 自动触发路由重算
系统 SHALL 在动态 cost revision 成功发布后自动运行路由计算。普通 cost 变化触发的路由重算默认至少间隔 30 秒且可配置；边状态变化以及所有协议过期导致的紧急降级必须绕过普通最小间隔。

#### Scenario: 普通 cost revision 触发重算
- **WHEN** 新 cost revision 由已经通过防抖的普通 cost 变化发布，且最小重算间隔已满足
- **THEN** 系统使用该 revision 自动计算最新路由

#### Scenario: 首份动态 cost 替换启动 fallback
- **WHEN** 一条边首次发布动态 cost，而当前路由仍可能使用无快照的 cost 1000
- **THEN** 系统立即使用首份动态 cost 重算，不等待普通最小重算间隔

#### Scenario: 边变为不可用
- **WHEN** 一条当前可用边的 `status` 变为不可用
- **THEN** 系统立即重新计算路由并排除该边，不等待普通最小重算间隔

#### Scenario: 边恢复可用
- **WHEN** 一条边的 `status` 恢复为 1
- **THEN** 系统立即使用其当前动态 cost 重算；没有快照时使用 cost 1000

#### Scenario: 指标全部过期
- **WHEN** 一条边因所有协议过期而立即发布 cost 1000
- **THEN** 系统允许该紧急 revision 立即触发路由重算

### Requirement: revision 一致的图快照
系统 MUST 使用同一个已提交 cost revision 的静态拓扑、边状态和当前动态 cost 构造完整图，不得让一次路由计算混用事务提交前后的边快照。

#### Scenario: 批量 cost 原子生效
- **WHEN** 一个 revision 同时更新多条边的 cost
- **THEN** 路由计算看到该批更新全部生效或全部未生效，不得看到部分更新

### Requirement: 原子发布最新路由结果
系统 SHALL 仅在完整路由计算成功后原子替换最新结果。最新结果必须包含对应 cost revision、路由版本和计算时间，并可通过 API 查询。

#### Scenario: 成功计算新路由
- **WHEN** Floyd 使用一个完整图成功计算所有目标结果
- **THEN** 系统原子发布带 revision 和计算时间的新路由结果

#### Scenario: 路由计算失败
- **WHEN** 构图或 Floyd 计算失败
- **THEN** 系统保留上一份完整路由结果并报告错误，不得发布半成品

### Requirement: 手动计算与最新结果查询
系统 SHALL 保留手动 `/calculate` 入口，并提供查询自动计算最新结果的 `/api/routes/latest`。手动计算也必须遵守边状态和动态 cost 构图规则。

#### Scenario: 查询最新路由
- **WHEN** 用户请求最新路由结果
- **THEN** 系统返回当前完整结果及其 cost revision、路由版本和计算时间

#### Scenario: 手动强制计算
- **WHEN** 用户调用 `/calculate`
- **THEN** 系统使用当前一致的动态图执行一次计算并返回结果

### Requirement: 图页面展示动态状态
系统 SHALL 在图和测试页面展示当前最终动态 cost、协议 cost、降级状态和最近指标时间，同时明确区分静态配置 cost。边的可用性颜色仍由 `graph_edges.status` 决定。

#### Scenario: 展示降级但可用的边
- **WHEN** 一条边 `status == 1` 且因指标全部过期而 cost 为 1000
- **THEN** 页面将其展示为可用边并标明指标降级，不得按不可用边着色

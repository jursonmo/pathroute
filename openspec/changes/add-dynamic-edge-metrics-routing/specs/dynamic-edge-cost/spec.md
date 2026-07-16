## ADDED Requirements

### Requirement: 按协议窗口聚合指标
系统 SHALL 按 `(from_node_id, to_node_id, proto)` 聚合最近窗口内的指标，默认窗口为 10 秒且可配置，并分别计算延迟、丢包率和速率的算术平均值。TCP、UDP 样本不得进入同一个协议聚合结果。

#### Scenario: 聚合最近窗口
- **WHEN** 一个协议在最近 10 秒内存在多个有效样本
- **THEN** 系统使用窗口内样本的算术平均值生成协议聚合指标，并记录窗口范围和样本数

#### Scenario: 窗口内没有样本但尚未过期
- **WHEN** 最近平滑窗口内没有新样本，但最后有效样本尚未达到过期阈值
- **THEN** 系统保留最近成功发布的协议或边 cost，不得仅因单个空窗口立即降级

### Requirement: 可替换的协议 cost 计算
系统 SHALL 通过 `CostCalculator` 抽象按指标协议选择计算方法。首版 TCP cost 等于平均延迟毫秒数；UDP cost 等于平均延迟毫秒数加平均丢包率乘以默认惩罚系数 1000。计算结果必须四舍五入并限制在 `1～1000`，公式版本和惩罚系数必须可追踪。

#### Scenario: 计算 TCP cost
- **WHEN** TCP 聚合指标的平均延迟为 35 毫秒
- **THEN** TCP 计算器返回 cost 35

#### Scenario: 计算 UDP cost
- **WHEN** UDP 平均延迟为 30 毫秒且平均丢包率为 0.05
- **THEN** UDP 计算器按 `30 + 0.05 × 1000` 返回 cost 80

#### Scenario: 限制 cost 范围
- **WHEN** 公式结果小于 1 或大于 1000
- **THEN** 系统将协议 cost 限制为 1 或 1000

### Requirement: 多协议 cost 合并
系统 SHALL 通过可替换的 `EdgeCostCombiner` 合并同一有向边当前有效的协议 cost。TCP、UDP 都有效时取算术平均；只有一个协议有效时直接使用该协议；没有有效协议时最终 cost 为 1000。

#### Scenario: 合并 TCP 和 UDP cost
- **WHEN** 同一边的有效 TCP cost 为 30、有效 UDP cost 为 70
- **THEN** 最终边 cost 为 50

#### Scenario: UDP 过期但 TCP 有效
- **WHEN** TCP cost 有效而 UDP 指标已经过期
- **THEN** 最终边 cost 等于 TCP cost，过期 UDP 不得以 1000 参与平均

#### Scenario: 所有协议过期
- **WHEN** 一条边没有任何当前有效的协议 cost
- **THEN** 最终边 cost 为 1000，并将快照标记为降级

### Requirement: 可配置的指标过期
系统 SHALL 在协议最后有效样本距当前时间达到默认 30 秒时将该协议标记为过期，过期阈值必须可配置。指标过期不得修改 `graph_edges.status`。

#### Scenario: 协议达到过期阈值
- **WHEN** 一个协议连续 30 秒没有有效新样本
- **THEN** 系统将该协议排除出边 cost 合并，并在协议快照中记录过期状态

#### Scenario: 指标恢复
- **WHEN** 已过期协议重新产生合法样本并形成有效聚合结果
- **THEN** 系统恢复该协议的 cost 计算资格，但不修改边状态

### Requirement: 普通 cost 变化防抖
系统 SHALL 将候选 cost 与当前已发布 cost 比较，默认在相对变化达到 10% 且连续确认 3 次后发布普通变化；阈值和确认次数必须可配置。候选状态可保存在内存中，进程重启后允许重新累计。

#### Scenario: 变化未达到阈值
- **WHEN** 候选 cost 相对当前已发布 cost 的变化小于 10%
- **THEN** 系统不创建新 publication、不修改路由使用的 cost 或 revision，也不触发普通路由重算；但必须刷新最新指标时间和协议状态

#### Scenario: 连续确认普通变化
- **WHEN** 候选 cost 变化达到 10% 并连续 3 次满足条件
- **THEN** 系统发布新的动态 cost 快照

#### Scenario: 所有协议过期的紧急降级
- **WHEN** 最后一个有效协议达到过期阈值
- **THEN** 系统立即发布 cost 1000，不等待普通连续确认

### Requirement: 原子持久化 cost 快照
系统 SHALL 在一个 MySQL 事务中创建 publication revision，并原子更新协议级和最终边 cost 快照。快照必须记录 cost、降级状态、聚合窗口、最后指标时间、计算时间、发布时间、公式版本和 revision。

#### Scenario: 成功发布一批 cost
- **WHEN** 一批边 cost 满足发布条件且 MySQL 事务提交成功
- **THEN** publication、协议快照和边快照使用同一个 revision 对外可见

#### Scenario: 快照事务失败
- **WHEN** 任一快照写入导致事务失败
- **THEN** 系统回滚整批发布，保留上一批成功快照，并且不得触发该 revision 的路由重算

### Requirement: 存储故障保留最后成功 cost
系统 MUST 在 ClickHouse 单次查询失败时保留最后成功发布的 cost，不得把查询错误直接解释为指标过期；只有超过配置的过期阈值仍未取得有效指标时才执行过期降级。

#### Scenario: 单次聚合查询失败
- **WHEN** ClickHouse 暂时无法返回聚合数据
- **THEN** 系统保留当前已发布 cost 并记录错误，不立即发布 1000

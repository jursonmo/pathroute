// Package nodemetric 提供有向边指标采集、动态 cost 计算和路由触发所需的领域模型。
//
// 包内模型不依赖 HTTP、GORM 或 ClickHouse 驱动，数据源与存储通过小接口接入，
// 以便测试模拟器和未来真实节点上报复用同一条业务链路。
package nodemetric

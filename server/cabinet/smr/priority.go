package smr

// 【单节点权重状态】
// 每个节点自己维护一份 PriorityState：
//   - leader：存自己当前的权重值（scheme[0]，始终最高）
//   - follower：每次收到 RPC 时，leader 会带来新的权重值，follower 更新自己

import (
	"errors"
	"sync"
)

// PriorityState 记录本节点当前的权重信息
type PriorityState struct {
	sync.RWMutex
	PrioClock int     // 当前所在轮次（单调递增，不能倒退）
	PrioVal   float64 // 本节点在当前轮次的权重值
	Majority  float64 // quorum 阈值（= 所有节点权重之和 / 2），超过此值即达成共识
}

// NewServerPriority 创建初始权重状态
func NewServerPriority(initPrioClock int, initPrioVal float64) PriorityState {
	return PriorityState{
		PrioClock: initPrioClock,
		PrioVal:   initPrioVal,
	}
}

// UpdatePriority 更新本节点的权重到新轮次
// 规则：pClock 只能前进，不能倒退（保证单调性，防止旧轮次的 RPC 覆盖新数据）
func (p *PriorityState) UpdatePriority(newPClock int, newPriority float64) error {
	p.Lock()
	defer p.Unlock()
	if newPClock < p.PrioClock {
		return errors.New("newPClock is less than current PClock")
	}
	p.PrioClock = newPClock
	p.PrioVal = newPriority
	return nil
}

// GetPriority 读取当前轮次和权重值
func (p *PriorityState) GetPriority() (pClock int, pValue float64) {
	p.RLock()
	defer p.RUnlock()
	return p.PrioClock, p.PrioVal
}

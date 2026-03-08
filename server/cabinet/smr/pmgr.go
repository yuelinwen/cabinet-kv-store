package smr

// 【权重管理器】（PriorityManager）
//
// Cabinet 算法核心：每轮共识结束后，按 follower 的回复速度重新排名，
// 越快回复的 follower 下一轮权重越高。这样"快节点"的票更重，系统性能更好。
//
// 权重方案（scheme）是一个几何级数，满足两个不变式：
//   I1：任意 t+1 个节点的权重之和 > majority（保证 quorum 总能达成）
//   I2：任意 t 个节点的权重之和   < majority（保证不足 quorum 无法提交）

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

type serverID = int
type prioClock = int
type priority = float64

// PriorityManager 管理每个 pClock 轮次下各节点的权重分配
// m[pClock][serverID] = 该节点在该轮次的权重
type PriorityManager struct {
	sync.RWMutex
	m        map[prioClock]map[serverID]priority // 每轮的权重表
	scheme   []priority                          // 权重模板（从高到低排列）
	majority float64                             // quorum 阈值
	n        int                                 // 节点总数
	q        int                                 // quorum 大小（= t+1）
}

// Init 初始化权重管理器，计算几何级数权重方案
// numOfServers  = 节点总数（3）
// quorumSize    = 最小 quorum 数（= t+1 = 2）
// baseOfPriorities = 权重基数（10）
// ratioTryStep  = 求解公比 r 的步长（0.01）
// isCab         = true 时用 Cabinet 几何方案，false 时退化为 Raft 等权
func (pm *PriorityManager) Init(numOfServers, quorumSize, baseOfPriorities int, ratioTryStep float64, isCab bool) {
	pm.n = numOfServers
	pm.q = quorumSize
	pm.m = make(map[prioClock]map[serverID]priority)

	// 计算满足 Cabinet I1/I2 不变式的公比 r
	ratio := 1.0
	if isCab {
		ratio = calcInitPrioRatio(numOfServers, quorumSize, ratioTryStep)
	}
	fmt.Println("[Cabinet] priority ratio:", ratio)

	// 生成初始权重表：serverID 0（leader）权重最高，依次递减
	newPriorities := make(map[serverID]priority)
	for i := 0; i < numOfServers; i++ {
		p := float64(baseOfPriorities) * math.Pow(ratio, float64(i))
		newPriorities[numOfServers-1-i] = p
		pm.scheme = append(pm.scheme, p)
	}
	reverseSlice(pm.scheme) // 从小到大变为从大到小

	pm.majority = sum(pm.scheme) / 2 // quorum 阈值 = 总权重 / 2

	pm.Lock()
	pm.m[0] = newPriorities // 存第 0 轮（初始轮）的权重表
	pm.Unlock()
}

func reverseSlice(slice []priority) {
	length := len(slice)
	for i := 0; i < length/2; i++ {
		j := length - 1 - i
		slice[i], slice[j] = slice[j], slice[i]
	}
}

// calcInitPrioRatio 求解满足 Cabinet I1/I2 不变式的几何公比 r
// 从 r=2.0 开始逐步缩小，找到第一个满足条件的值
func calcInitPrioRatio(n, f int, ratioTryStep float64) (ratio float64) {
	r := 2.0
	for {
		// I1: 最强 t+1 个节点之和 > majority
		// I2: 最强 t 个节点之和   < majority
		if math.Pow(r, float64(n-f+1)) > 0.5*(math.Pow(r, float64(n))+1) &&
			0.5*(math.Pow(r, float64(n))+1) > math.Pow(r, float64(n-f)) {
			return r
		}
		r -= ratioTryStep
	}
}

func sum(arr []float64) float64 {
	total := 0.0
	for _, val := range arr {
		total += val
	}
	return total
}

// UpdateFollowerPriorities 每轮共识结束后，根据本轮 follower 的回复顺序重新分配权重
// prioQueue 是本轮回复顺序（channel 里越靠前 = 越快回复）
// 快的 follower 下一轮权重更高 → 系统自动倾向选择更快的节点
func (pm *PriorityManager) UpdateFollowerPriorities(pClock prioClock, prioQueue chan serverID, leaderID serverID) error {
	newPriorities := make(map[serverID]priority)
	arranged := make(map[serverID]bool)

	for i := 0; i < pm.n; i++ {
		arranged[i] = false
	}

	nr := len(prioQueue) // 本轮有多少 follower 回复了

	// 按回复顺序分配权重：最快的 follower 得 scheme[1]，次快得 scheme[2]……
	for i := 0; i < nr; i++ {
		s := <-prioQueue
		newPriorities[s] = pm.scheme[i+1] // scheme[0] 留给 leader
		arranged[s] = true
	}

	i := nr + 1

	// 没有回复的 follower 分配剩余的最低权重
	for id, done := range arranged {
		if !done {
			if id == leaderID {
				newPriorities[id] = pm.scheme[0] // leader 始终保持最高权重
				continue
			}
			if i == len(pm.scheme) {
				err := fmt.Sprintf("priority assignment of [%v] exceeds pm scheme length [%v]", i, len(pm.scheme))
				return errors.New(err)
			}
			newPriorities[id] = pm.scheme[i]
			i++
		}
	}

	pm.Lock()
	pm.m[pClock] = newPriorities // 存入下一轮的权重表
	pm.Unlock()
	return nil
}

// GetFollowerPriorities 返回指定轮次下所有节点的权重表
func (pm *PriorityManager) GetFollowerPriorities(pClock int) (fpriorities map[serverID]priority) {
	fpriorities = make(map[serverID]priority)
	pm.RLock()
	defer pm.RUnlock()
	fpriorities = pm.m[pClock]
	return
}

func (pm *PriorityManager) GetMajority() float64        { return pm.majority }
func (pm *PriorityManager) GetPriorityScheme() []priority { return pm.scheme }
func (pm *PriorityManager) GetQuorumSize() int           { return pm.q }

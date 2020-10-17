package main

import (
	"sort"

	log "github.com/sirupsen/logrus"
)

//Difficulty - DifficultyAlgorithm is the calculateDifficulty() function type DifficultyAlgorithm func(bool, Blockchain) float64
type Difficulty interface {
	getDiff(bool, Blockchain) float64
	//Parse(data []byte) error
}

//this can be optimized by using sort.Sort and using the timestamp
//as parameter
//https://stackoverflow.com/questions/36122668/golang-how-to-sort-struct-with-multiple-sort-parameters
func median(blocks []Block) Block {
	num := len(blocks)
	var times []int
	for _, block := range blocks {
		times = append(times, block.timestamp)
	}
	sort.Ints(times)
	med := times[num/2]
	for _, block := range blocks {
		if med == block.timestamp {
			return block
		}
	}
	log.Error("Median has reached a point with no match")
	return blocks[0]
}

func sumBlocks(input ...Block) float64 {
	var works []float64
	for _, w := range input {
		works = append(works, w.difficulty)
	}
	return sum(works...)
}

func sum(input ...float64) (sum float64) {
	sum = 0

	for _, n := range input {
		sum += n
	}

	return
}

type bchDifficulty struct {
	Lookback       int  `yaml:"lookback" json:"lookback"`
	OffByOne       bool `yaml:"offbyone" json:"offbyone"`
	Mediantimepast int  `yaml:"mediantimepast" json:"mediantimepast"`
}

//func bchCalculateDifficulty(isPrivate bool, blockchain Blockchain) float64 {
func (b bchDifficulty) getDiff(isPrivate bool, blockchain Blockchain) float64 {
	var chain []Block
	if isPrivate {
		chain = blockchain.getPrivateView()
	} else {
		chain = blockchain.chain
	}

	chainLen := len(chain)
	top3 := chain[chainLen-b.Mediantimepast:]
	bottom3 := chain[chainLen-b.Lookback-b.Mediantimepast : chainLen-b.Lookback]
	topMed := median(top3)
	bottomMed := median(bottom3)

	//Need +1 because slice indexing is exclusive
	subChain := chain[bottomMed.height : topMed.height+1]

	totalTime := topMed.timestamp - bottomMed.timestamp
	totalWork := sumBlocks(subChain...)

	//Hi-lo filter
	//totalTime cannot be greater than 2 days (288 blocks) or lower than 0.5 days (72 blocks)
	if totalTime > 2*b.Lookback*blockchain.expectedBlockTime {
		totalTime = 2 * b.Lookback * blockchain.expectedBlockTime
	} else if totalTime < b.Lookback*blockchain.expectedBlockTime/2 {
		totalTime = b.Lookback * blockchain.expectedBlockTime / 2
	}

	newDiff := (totalWork * float64(blockchain.expectedBlockTime)) / float64(totalTime)

	if newDiff < 0.1 {
		log.WithFields(log.Fields{
			"Height":      blockchain.height,
			"Fork height": blockchain.forkHeight,
			"Total work":  totalWork,
			"Total time":  totalTime,
			"NewDiff":     newDiff,
		}).Fatal("Difficulty adjustment new difficulty <.1")
	}

	return newDiff
}

type btcDifficulty struct {
	Period   int  `yaml:"period" json:"period"`
	OffByOne bool `yaml:"offbyone" json:"offbyone"`
}

//func btcCalculateDifficulty(isPrivate bool, blockchain Blockchain) float64 {
func (b btcDifficulty) getDiff(isPrivate bool, blockchain Blockchain) float64 {
	var chain []Block
	if isPrivate {
		chain = blockchain.getPrivateView()
	} else {
		chain = blockchain.chain
	}
	chainLen := len(chain)

	if (chainLen-startingBlocks)%b.Period != 0 {
		return chain[chainLen-1].difficulty
	}
	top := chain[chainLen-1]
	botBlock := chainLen - b.Period
	if !b.OffByOne {
		botBlock--
	}

	bot := chain[botBlock]
	totalTime := top.timestamp - bot.timestamp
	totalWork := sumBlocks(chain[chainLen-b.Period : chainLen-1]...) // May not want -1?
	if totalTime > (b.Period * 4 * blockchain.expectedBlockTime) {
		totalTime = b.Period * 4 * blockchain.expectedBlockTime
	} else if totalTime < ((b.Period / 4) * blockchain.expectedBlockTime) {
		totalTime = (b.Period / 4) * blockchain.expectedBlockTime
	}

	newDiff := totalWork * float64(blockchain.expectedBlockTime) / float64(totalTime)
	return newDiff
}

type dashDifficulty struct {
	NPastBlocks int  `yaml:"npastblocks" json:"npastblocks"`
	OffByOne    bool `yaml:"offbyone" json:"offbyone"`
}

func (d dashDifficulty) getDiff(isPrivate bool, blockchain Blockchain) float64 {
	var chain []Block
	nPastBlocks := d.NPastBlocks

	var pIndexLast int
	if isPrivate {
		var privBranchLen = len(blockchain.privateBranch)
		pIndexLast = blockchain.privateBranch[privBranchLen-1].height
		chain = blockchain.getPrivateView()
	} else {
		chain = blockchain.chain
		pIndexLast = blockchain.height
	}

	pIndex := pIndexLast
	var bnTarget, bnPastTargetAvg float64
	for nBlock := 1; nBlock <= nPastBlocks; nBlock++ {
		//Recent change
		bnTarget = 1.0 / chain[pIndex].difficulty
		if nBlock == 1 {
			bnPastTargetAvg = bnTarget
		} else {
			bnPastTargetAvg = (bnPastTargetAvg*float64(nBlock) + bnTarget) / float64(nBlock+1)
		}
		if nBlock != nPastBlocks {
			pIndex--
		}
	}

	bnNew := bnPastTargetAvg
	if !d.OffByOne {
		pIndex--
	}
	nActualTimestamp := chain[pIndexLast].timestamp - chain[pIndex].timestamp
	nTargetTimestamp := blockchain.expectedBlockTime * nPastBlocks
	//fmt.Printf("ACTUAL TIME DIFF: %d\tEXPECTED DIFF: %d\n", nActualTimestamp, nTargetTimestamp)

	if nActualTimestamp > nTargetTimestamp*3 {
		nActualTimestamp = nTargetTimestamp * 3
	} else if float64(nActualTimestamp) < float64(nTargetTimestamp)/3.0 {
		nActualTimestamp = nTargetTimestamp / 3
	}

	bnNew = bnNew * (float64(nActualTimestamp) / float64(nTargetTimestamp))
	return 1.0 / bnNew
}

type xmrDifficulty struct {
	Lookback int `yaml:"lookback" json:"lookback"`
	Delay    int `yaml:"delay" json:"delay"`
	Outliers int `yaml:"outliers" json:"outliers"`
}

//Section 6.2.4: https://ww.getmonero.org/library/Zero-to-Monero-1-0-0.pdf
//https://github.com/monero-project/monero/blob/16dc6900fb556b61edaba5e323497e9b8c677ae2/src/cryptonote_core/blockchain.cpp <-- get_difficulty_next_block()
//https://github.com/monero-project/monero/blob/16dc6900fb556b61edaba5e323497e9b8c677ae2/src/cryptonote_basic/difficulty.cpp <-- next_difficulty()
func (x xmrDifficulty) getDiff(isPrivate bool, blockchain Blockchain) float64 {
	var chain []Block
	if isPrivate {
		chain = blockchain.getPrivateView()
	} else {
		chain = blockchain.chain
	}

	chainHeight := len(chain) - 1

	examineChain := make([]Block, x.Lookback)
	//examineChain = chain[chainHeight-720-15 : chainHeight-15]

	j := 0
	for i := chainHeight - x.Lookback - x.Delay; i < chainHeight-x.Delay; i++ {
		examineChain[j] = chain[i]
		j++
	}

	// The timestamps should be sorted, but we need an array of unsorted difficulties.

	timestampList := make([]int, x.Lookback)

	for i := 0; i < x.Lookback; i++ {
		timestampList[i] = examineChain[i].timestamp
	}
	sort.Ints(timestampList)
	timestampListOutliersRemoved := timestampList[x.Outliers : len(timestampList)-x.Outliers]
	timeSpan := timestampListOutliersRemoved[len(timestampListOutliersRemoved)-1] - timestampListOutliersRemoved[0]

	// remove outliers from diffList and calculate cumulative difficulties
	diffOutliersRemoved := examineChain[x.Outliers : len(examineChain)-x.Outliers]

	totalWork := sumBlocks(diffOutliersRemoved...)

	target := blockchain.expectedBlockTime
	newDiff := (totalWork * float64(target)) / float64(timeSpan)

	return newDiff
}

type zecDifficulty struct {
	NAveragingInterval  int     `yaml:"navginterval" json:"navginterval"`
	NMedianTimespan     int     `yaml:"nmediantimespan" json:"nmediantimespan"`
	NMaxAdjustUp        int     `yaml:"nmaxadjustup" json:"nmaxadjustup"`
	NMaxAdjustDown      int     `yaml:"nmaxadjustdown" json:"nmaxadjustdown"`
	NPOWDampeningFactor float64 `yaml:"npowdampeningfactor" jsob:"npowdampeningfactor"`
}

func (z zecDifficulty) getDiff(isPrivate bool, blockchain Blockchain) float64 {
	var chain []Block
	if isPrivate {
		chain = blockchain.getPrivateView()
	} else {
		chain = blockchain.chain
	}

	nAveragingInterval := z.NAveragingInterval
	nMediantimespan := z.NMedianTimespan
	nMaxAdjustUp := z.NMaxAdjustUp
	nMaxAdjustDown := z.NMaxAdjustDown
	nPOWDampeningFactor := z.NPOWDampeningFactor
	nAveragingTargetTimespan := nAveragingInterval * blockchain.expectedBlockTime
	nMinActualTimespanV3 := float64(nAveragingTargetTimespan) * float64(100-nMaxAdjustUp) / 100.0
	nMaxActualTimespanV3 := float64(nAveragingTargetTimespan) * float64(100+nMaxAdjustDown) / 100.0

	B := chain[len(chain)-1]
	A := chain[len(chain)-1-nAveragingInterval]

	//get median of past 11 (including B)
	bMedian := median(chain[B.height-nMediantimespan : B.height])

	//get median of past 11 (including A)
	aMedian := median(chain[A.height-nMediantimespan : A.height])

	nActualTimespan := bMedian.timestamp - aMedian.timestamp
	nActualTimespanf := float64(nAveragingTargetTimespan) + float64(nActualTimespan-nAveragingTargetTimespan)/nPOWDampeningFactor

	if nActualTimespanf < nMinActualTimespanV3 {
		nActualTimespanf = nMinActualTimespanV3
	} else if nActualTimespanf > nMaxActualTimespanV3 {
		nActualTimespanf = nMaxActualTimespanV3
	}

	nAvgTarget := 0.0
	for _, block := range chain[B.height-nAveragingInterval : B.height] {
		//nAvgTarget += block.difficulty
		nAvgTarget += float64(1.0) / float64(block.difficulty)
	}
	nAvgTarget /= float64(nAveragingInterval)

	bnNew := nAvgTarget / float64(nAveragingTargetTimespan)
	bnNew = bnNew * nActualTimespanf

	//fmt.Printf("--------------------------------ZEC DIFF--------------------------------------------\n")
	log.WithFields(log.Fields{
		"B":                B,
		"A":                A,
		"bMedian":          bMedian,
		"aMedian":          aMedian,
		"Actual timespan":  nActualTimespan,
		"Actual timespanF": nActualTimespanf,
		"nAvgTarget":       nAvgTarget,
		"bnNew":            bnNew,
		"1/bnNew":          float64(1.0) / float64(bnNew),
		"TOP30":            chain[len(chain)-30:],
	}).Info("Difficulty Change")

	return 1.0 / bnNew
}

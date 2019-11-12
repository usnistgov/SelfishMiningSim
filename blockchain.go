package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"
)

//Should these be included in the new "difficultyParmaters"?
const BASElINE_DIFFICULTY = 1.0
const STARTING_BLOCKS = 3000

//Block holds information on a block: its height, difficulty and timestamp
type Block struct {
	height     int
	difficulty float64
	timestamp  int
	isHonest   bool
}

type byTimestamp []Block

func (s byTimestamp) Len() int {
	return len(s)
}
func (s byTimestamp) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byTimestamp) Less(i, j int) bool {
	return s[i].timestamp < s[j].timestamp
}

type byDiff []Block

func (s byDiff) Len() int {
	return len(s)
}
func (s byDiff) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byDiff) Less(i, j int) bool {
	return s[i].difficulty < s[j].difficulty
}

type byHeight []Block

func (s byHeight) Len() int {
	return len(s)
}
func (s byHeight) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s byHeight) Less(i, j int) bool {
	return s[i].height < s[j].height
}

//Blockchainer provides the methods a blockchain must implement
type Blockchainer interface {
	Init()
	Reset()
	stats() (int, int, float64)
	printStats()
	pushToChain(Block)
	pushToPrivateChain(Block)
	popFromChain() Block
	popFromPrivateChain() Block
	popFromPrivateChainBottom() Block
	getPostForkWork() (float64, float64)
	setForkHeight(int)
	getPrivateView() []Block
	//calculateDifficulty(bool) float64
	adjustDifficulty(bool)
	newBlock(int)
	newPrivateBlock(int)
	reorg()
	reorgRace()
	//setDiffAlgo(DifficultyAlgorithm)
	setDiffAlgo(Difficulty)
}

//Perhaps blockchain struct should only hold data for 1 chain (therefore
// the simulation will hold 2 - one for public one for private

type Blockchain struct {
	chain                 []Block
	privateBranch         []Block
	forkHistory           []int
	height                int
	forkHeight            int
	time                  int
	privateTime           int
	nextDifficulty        float64
	nextPrivateDifficulty float64
	//diffAlgo              DifficultyAlgorithm
	diffAlgo          Difficulty
	expectedBlockTime int
}

//Init initializes a blockchain with 150 blocks all with normal difficulty and time.
func (blockchain *Blockchain) Init() {
	blockchain.height = -1 //Set to -1 since we are about to add genesis (0)
	for i := 0; i < STARTING_BLOCKS; i++ {
		blockchain.pushToChain(Block{
			i, BASElINE_DIFFICULTY, i * blockchain.expectedBlockTime, true})
	}
	blockchain.forkHeight = 0
	blockchain.nextDifficulty = BASElINE_DIFFICULTY
	blockchain.nextPrivateDifficulty = BASElINE_DIFFICULTY
	//blockchain.chainType = unassigned
}

//func (blockchain *Blockchain) setDiffAlgo(diffAlgo DifficultyAlgorithm) {
func (blockchain *Blockchain) setDiffAlgo(diffAlgo Difficulty) {
	blockchain.diffAlgo = diffAlgo
}

//Reset will reset the blockchain to empty and then call Init()
func (blockchain *Blockchain) Reset() {
	blockchain.chain = nil
	blockchain.privateBranch = nil
	blockchain.forkHistory = nil
	blockchain.privateTime = 0
	blockchain.nextDifficulty = 0.0
	blockchain.nextPrivateDifficulty = 0.0
	blockchain.Init()
}

func (blockchain *Blockchain) stats() (sm, total int, winRatio float64) {
	hm := 0
	sm = 0
	for _, block := range blockchain.chain[STARTING_BLOCKS:] {
		if block.isHonest {
			hm++
		} else {
			sm++
		}
	}

	total = hm + sm
	winRatio = float64(sm) / float64(total)
	return
}

//printStats will print statistics about the blockchain.
func (blockchain *Blockchain) printStats() {
	fmt.Printf("Height: %d\n", blockchain.height)
	//fmt.Printf("Current time: %d\n", blockchain.time)
	fmt.Printf("Private length: %d\tFork height: %d\n", len(blockchain.privateBranch), blockchain.forkHeight)
}

//pushToChain will push the given block to the public blockchain.
func (blockchain *Blockchain) pushToChain(block Block) {
	blockchain.chain = append(blockchain.chain, block)
	blockchain.height++
	if block.height != blockchain.height {
		log.WithFields(log.Fields{
			"Block": block.height,
			"Chain": blockchain.height,
		}).Warn("New block and new height don't match")
	}
	blockchain.time = block.timestamp
}

//pushToPrivateChain pushes a block to the private branch.
func (blockchain *Blockchain) pushToPrivateChain(block Block) {
	blockchain.privateBranch = append(blockchain.privateBranch, block)
	blockchain.time = block.timestamp
	log.WithFields(log.Fields{
		"Block":               block,
		"Tip":                 blockchain.chain[len(blockchain.chain)-1],
		"PrivateBranchLength": len(blockchain.privateBranch),
	}).Info("NEW PRIVATAE BLOCK")
}

//popFromChain will remove AND return the tip (most recent) block of the chain.
func (blockchain *Blockchain) popFromChain() Block {
	//removedBlock := blockchain.chain[:blockchain.height][0]
	removedBlock := blockchain.chain[blockchain.height]
	blockchain.chain = blockchain.chain[:blockchain.height]
	blockchain.height--
	return removedBlock
}

//popFromPrivateChain will remove AND return the tip (most recent) block of the private branch.
func (blockchain *Blockchain) popFromPrivateChain() Block {
	removedBlock := blockchain.privateBranch[len(blockchain.privateBranch)-1]
	blockchain.privateBranch = blockchain.privateBranch[:len(blockchain.privateBranch)-1]
	return removedBlock
}

//popFromPrivateChainBottom will remove AND return the first block in the private branch
func (blockchain *Blockchain) popFromPrivateChainBottom() Block {
	removedBlock := blockchain.privateBranch[0]
	blockchain.privateBranch = blockchain.privateBranch[1:]
	return removedBlock
}

//clearPrivateBranch will remove all private branch blocks
func (blockchain *Blockchain) clearPrivateBrach() {
	for len(blockchain.privateBranch) > 0 {
		blockchain.popFromPrivateChain()
	}
}

//getPostForkWork will return the amount of work for the public chain and private branch
//it only includes work past the fork.
func (blockchain Blockchain) getPostForkWork() (mainWork, privWork float64) {
	mainWork, privWork = 0.0, 0.0
	if blockchain.forkHeight == 0 {
		return
	}
	for _, thisBlock := range blockchain.chain[blockchain.forkHeight+1:] {
		mainWork += thisBlock.difficulty
	}

	for _, thisBlock := range blockchain.privateBranch {
		privWork += thisBlock.difficulty
	}

	return
}

//setForkHeight sets the height of where a fork occurs.
//	offset is 0 if we are no longer forking
//	offset is -1 if we have just forked
//	else, offset adds
func (blockchain *Blockchain) setForkHeight(offset int) {
	if offset == 0 {
		blockchain.forkHeight = 0
	} else if offset > 0 {
		blockchain.forkHeight += offset
	} else {
		blockchain.forkHeight = blockchain.height
	}
	blockchain.forkHistory = append(blockchain.forkHistory, blockchain.forkHeight)
}

//getPrivateView returns the entire blockchain from the view of the private branch.
func (blockchain *Blockchain) getPrivateView() []Block {
	var chain []Block
	for i := 0; i <= blockchain.forkHeight; i++ {
		chain = append(chain, blockchain.chain[i])
	}

	chain = append(chain, blockchain.privateBranch...)
	return chain
}

//newBlock creates a new block and pushes it to the chain.
func (blockchain *Blockchain) newBlock(time int) Block {
	block := Block{blockchain.height + 1, blockchain.nextDifficulty, time, true}
	blockchain.pushToChain(block)
	blockchain.adjustDifficulty(false)
	return block
}

//newPrivateBlock creates a new block and pushes it to the private branch.
func (blockchain *Blockchain) newPrivateBlock(time int) Block {
	privBranchLen := len(blockchain.privateBranch)

	var newHeight int
	if privBranchLen > 0 {
		newHeight = blockchain.privateBranch[privBranchLen-1].height + 1
	} else {
		newHeight = blockchain.height + 1
		//blockchain.setForkHeight(-1)
	}
	block := Block{newHeight, blockchain.nextPrivateDifficulty, time, false}
	blockchain.pushToPrivateChain(block)
	blockchain.adjustDifficulty(true)
	return block
}

//reorg will replace the main chain with the private branch
func (blockchain *Blockchain) reorg() {
	numOrphanBlocks := blockchain.height - blockchain.forkHeight

	log.WithFields(log.Fields{
		"Main height":       blockchain.height,
		"Fork height":       blockchain.forkHeight,
		"Priv length":       len(blockchain.privateBranch),
		"Num orphan blocks": numOrphanBlocks,
	}).Info("Reorg called")
	//fmt.Printf("-----------REORG CALLED-----------------\n")

	//Remove every block since the fork
	for i := 0; i < numOrphanBlocks; i++ {
		blockchain.popFromChain()
	}
	for len(blockchain.privateBranch) > 0 {
		block := blockchain.popFromPrivateChainBottom()
		blockchain.pushToChain(block)
	}
	blockchain.setForkHeight(0)
	blockchain.nextDifficulty = blockchain.nextPrivateDifficulty
}

//regorgRace is called when the SM has a lead and wants to orphan an honest block
//therefore we will pop the tip of the honest chain and add the 2 earliest fork blocks
func (blockchain *Blockchain) reorgRace() {
	log.WithFields(log.Fields{
		"Main height": blockchain.height,
		"Priv length": len(blockchain.privateBranch),
	}).Info("Reorg called")
	blockchain.popFromChain()
	blockchain.pushToChain(blockchain.popFromPrivateChainBottom())
	blockchain.pushToChain(blockchain.popFromPrivateChainBottom())
	blockchain.setForkHeight(-1)
	blockchain.adjustDifficulty(false)
}

func (blockchain *Blockchain) adjustDifficulty(isPrivate bool) {
	newDiff := blockchain.diffAlgo.getDiff(isPrivate, *blockchain)
	if isPrivate {
		blockchain.nextPrivateDifficulty = newDiff
	} else {
		blockchain.nextDifficulty = newDiff
	}
}

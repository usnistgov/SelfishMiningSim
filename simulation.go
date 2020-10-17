package main

import (
	"fmt"
	"math"

	"time"

	//?move to math/rand?
	"golang.org/x/exp/rand"

	log "github.com/sirupsen/logrus"
	distuv "gonum.org/v1/gonum/stat/distuv"
)

/*
//State is the type of state we are in
type State int
*/

//Simulation holds the information regarding a certain simulation
type Simulation struct {
	blockchain            Blockchain
	expectedBlockTime     int
	startTime             int
	numSimBlocks          int //Number of blocks to simulate
	timewarpOffset        int //Timestamp offset if we are timewarping
	isTimewarp            bool
	isBCHStrategic        bool
	alpha                 float64
	honestRatio           float64 //Fraction of honest miners (1-alpha)
	gamma                 float64 //Fraction of honest miners that mine on SM block
	state                 int
	prevState             int
	effectiveState        float64 //(Work on priv branch) - (work on main chain) after fork
	ifLose                float64 //effectiveState - nextDifficulty
	realTime              int     //seconds since genesis
	stateHistory          []int
	effectiveStateHistory []float64
	simHistory            []float64
	ID                    int //Simulation ID
}

//SimulationResult holds the results of a simulation
type SimulationResult struct {
	WinRatio               float64 `json:"winratio"`
	AdjustedWinning        float64 `json:"adjustedwinning"`
	SelfishSecondsPerBlock float64 `json:"selfishsecondsperblock"`
	RelativeGain           float64 `json:"relativegain"`
	AdjustedRelativeGain   float64 `json:"adjustedrelativegain"`
	FinalHeight            int     `json:"finalheight"`
	NumReorgs              int     `json:"numreorgs"`
	SmWinReorgs            int     `json:"smwinreorgs"`
}

//Simulationer provides the methods a simulation must implement
type Simulationer interface {
	init(alpha float64, gamma float64, blocks, timewarp int, isTimewarp bool, id int)
	reset()
	setState(int)
	setRealTime(int)
	newPrivateBlock()
	getLambdas() (lambdaHonest, lambdaSelfish float64)
	getDelays() (delayHonest, delaySelfish int)
	runSimulation() (winRatio, adjustedWinning, selfishSecondsPerBlock float64)
	zeroToOne()
	oneToTwo(int)
	moreThanTwo(int)
	moreThanTwoHonestWins(int)
	smWinsRace(int)
	hmOnSm(int)
	hmWinsRace(int)
}

var randsrc rand.Source

//var START_TIME = 89400

//init will initialize the simulation with the given parameters
func (sim *Simulation) init(alpha float64, gamma float64, blocks, timewarp int, isBCHStrategic bool, diffAlgo Difficulty, expectedBlockTime int, id int) {
	sim.expectedBlockTime = expectedBlockTime
	sim.blockchain.expectedBlockTime = expectedBlockTime
	sim.blockchain.Init()
	sim.blockchain.setDiffAlgo(diffAlgo)
	sim.numSimBlocks = blocks
	sim.isBCHStrategic = isBCHStrategic
	sim.timewarpOffset = timewarp
	sim.alpha = alpha
	sim.honestRatio = 1 - sim.alpha
	sim.gamma = gamma
	sim.state = 0
	sim.effectiveState = 0.0
	sim.realTime = sim.blockchain.time
	rand.Seed(uint64(time.Now().UnixNano()))
	randsrc = rand.NewSource(uint64(time.Now().UTC().UnixNano()))
	sim.startTime = startingBlocks * expectedBlockTime
	sim.ID = id
}

func (sim *Simulation) reset() {
	sim.blockchain.Reset()
	sim.state = 0
	sim.effectiveState = 0.0
	sim.realTime = sim.blockchain.time
	sim.stateHistory = nil
	sim.effectiveStateHistory = nil
}

func (sim *Simulation) setState(state int) {
	//Add the if statement from python
	mainWork, privWork := sim.blockchain.getPostForkWork()
	sim.effectiveState = privWork - mainWork
	sim.effectiveStateHistory = append(sim.effectiveStateHistory, sim.effectiveState)
	//positive ifLose means if we lose the next block, we are still ahead
	sim.ifLose = sim.effectiveState - sim.blockchain.nextDifficulty
	privLen := len(sim.blockchain.privateBranch)
	sim.prevState = sim.state
	if sim.prevState == 1 && privLen > 0 && sim.effectiveState == 0 {
		sim.state = -1
	} else if state == -1 {
		sim.state = -1
	} else if privLen == 0 {
		sim.state = 0
	} else if sim.ifLose < 0 && privLen > 0 {
		sim.state = -1
	} else if sim.ifLose == 0 && privLen > 0 {
		sim.state = 1
	} else if sim.ifLose > 0 && privLen > 0 {
		sim.state = 2
	}

	log.WithFields(log.Fields{
		"mainWork":       mainWork,
		"privWork":       privWork,
		"effectiveState": sim.effectiveState,
		"ifLose":         sim.ifLose,
		"privLen":        privLen,
		"state":          sim.state,
	}).Info("State Change")

	sim.stateHistory = append(sim.stateHistory, sim.state)
}

func (sim *Simulation) setRealTime(timeOffset int) {
	sim.realTime += timeOffset
}

//What is strategic in python code?
func (sim *Simulation) newPrivateBlock() {
	sim.blockchain.newPrivateBlock(sim.realTime + sim.timewarpOffset)
}

//lambda is the rate of block generation (poisson process)
//getLambdas returns the lambdas for honest and selfish poisson
func (sim *Simulation) getLambdas() (lambdaHonest, lambdaSelfish float64) {
	lambdaHonest = 1.0 / ((sim.blockchain.nextDifficulty * float64(sim.expectedBlockTime)) / sim.honestRatio)
	lambdaSelfish = 1.0 / ((sim.blockchain.nextPrivateDifficulty * float64(sim.expectedBlockTime)) / sim.alpha)
	return
}

//getDelays returns the time for both honest and selfish miners to mine the next block
func (sim *Simulation) getDelays() (delayHonest, delaySelfish int) {
	lambdaHonest, lambdaSelfish := sim.getLambdas()
	distHonest := distuv.Exponential{
		Rate: lambdaHonest,
		Src:  randsrc,
	}
	distSelfish := distuv.Exponential{
		Rate: lambdaSelfish,
		Src:  randsrc,
	}

	delayHonest = int(distHonest.Rand())
	delaySelfish = int(distSelfish.Rand())
	return
}

func (sim *Simulation) runSimulation(resultChannel chan<- SimulationResult) {
	var res SimulationResult
	for (sim.blockchain.height < startingBlocks+sim.numSimBlocks) || (len(sim.blockchain.privateBranch) != 0) {
		//No private branch, both mining at the same tip
		privHeight := 0
		if len(sim.blockchain.privateBranch) > 0 {
			privHeight = len(sim.blockchain.getPrivateView())
		}
		log.WithFields(log.Fields{
			"Height": sim.blockchain.height,
			//"Type":   fmt.Sprintf("%T", sim.blockchain),
			"State":       sim.state,
			"Priv length": len(sim.blockchain.privateBranch),
			"Diff":        math.Floor(sim.blockchain.nextDifficulty*10000) / 10000,
			"Priv diff":   math.Floor(sim.blockchain.nextPrivateDifficulty*10000) / 10000,
			"Priv height": privHeight,
			"Length":      len(sim.blockchain.chain),
			"RealTime":    sim.realTime,
		}).Info("Simulating block")

		if sim.state == 0 {
			sim.blockchain.nextPrivateDifficulty = sim.blockchain.nextDifficulty
			lambd := 1.0 / (sim.blockchain.nextDifficulty * float64(sim.expectedBlockTime))
			//delay := generator.Poisson(lambd)
			delay := distuv.Exponential{
				Rate: lambd,
				Src:  randsrc,
			}.Rand()
			sim.setRealTime(int(delay))

			if rand.Float64() <= sim.alpha { //Selfish wins
				sim.zeroToOne()
			} else {
				sim.blockchain.newBlock(sim.realTime)
			}
			continue
		}

		//Selfish miner has a private chain of length 1 (pub behind 1)
		if sim.state == 1 {
			delayHonest, delaySelfish := sim.getDelays()
			if delaySelfish < delayHonest { //Selfish wins
				sim.oneToTwo(delaySelfish)
			} else { //Honest wins, selfish publishes and we race
				sim.setRealTime(delayHonest)
				sim.blockchain.newBlock(sim.realTime)
				sim.setState(-1) //-1 signals a RACE
			}
			continue
		}

		//Selfish has a lead of more than 1
		if sim.state >= 2 {
			delayHonest, delaySelfish := sim.getDelays()
			if delaySelfish < delayHonest { //Selfish wins
				sim.moreThanTwo(delaySelfish)
			} else { //Selfish broadcasts two blocks, orphaning the honest
				sim.moreThanTwoHonestWins(delayHonest)
			}
			continue
		}

		if sim.state == -1 {
			res.NumReorgs++
			delayHonest, delaySelfish := sim.getDelays()
			if delaySelfish < delayHonest {
				sim.smWinsRace(delaySelfish)
				res.SmWinReorgs++
			} else {
				if rand.Float64() < sim.gamma { //HM mines on SM block
					sim.hmOnSm(delayHonest)
				} else {
					sim.hmWinsRace(delayHonest)
				}
			}
			continue
		}
	}

	for i := 0; i <= sim.blockchain.height; i++ {
		block := sim.blockchain.chain[i]
		if i != block.height {
			fmt.Printf("MISMATCH: Real: %d Block: %d Honest: %t\n", i, block.height, block.isHonest)
		}
	}

	sm, _, winRatio := sim.blockchain.stats()
	elapsedTime := sim.realTime - sim.startTime
	timeRatio := float64(elapsedTime) / float64((sim.blockchain.height-startingBlocks)*sim.expectedBlockTime)
	res.WinRatio = winRatio
	res.AdjustedWinning = winRatio / timeRatio
	res.RelativeGain = (winRatio - sim.alpha) / sim.alpha
	res.AdjustedRelativeGain = (res.AdjustedWinning - sim.alpha) / sim.alpha

	if sm == 0 {
		res.SelfishSecondsPerBlock = -1
		resultChannel <- res
		return
	}
	res.SelfishSecondsPerBlock = float64(elapsedTime) / float64(sm)
	sim.simHistory = append(sim.simHistory, res.SelfishSecondsPerBlock)
	res.FinalHeight = sim.blockchain.height
	resultChannel <- res
	return
}

//zeroToOne occurs when a selfish miner has mined a block and STARTED a private branch
func (sim *Simulation) zeroToOne() {
	log.Debug("zeroToOne")
	sim.blockchain.setForkHeight(-1)
	sim.newPrivateBlock()
	sim.setState(1)
}

//oneToTwo occurs when selfish miner now has two blocks on private branch
func (sim *Simulation) oneToTwo(delay int) {
	log.Debug("oneToTwo")
	sim.setRealTime(delay)
	sim.newPrivateBlock()
	sim.setState(sim.state + 1)
}

func (sim *Simulation) moreThanTwo(delay int) {
	log.Debug("moreThantwo")
	sim.setRealTime(delay)
	sim.newPrivateBlock()
	sim.setState(sim.state + 1)
}

//moreThanTwoHonestWins occurs when selfish has a lead >= 2.
//	if lead =2, broadcast both blocks and cause a reorg.
//	if lead > 2, continue withholding (wasting miners resources)
func (sim *Simulation) moreThanTwoHonestWins(delay int) {
	log.Debug("moreThanTwoHonestWins")
	sim.setRealTime(delay)
	sim.blockchain.newBlock(sim.realTime)
	//sim.setState(sim.state - 2)
	sim.setState(0)
	//fmt.Printf("ifLose: %f\n", sim.ifLose)
	if sim.ifLose < 0 && len(sim.blockchain.privateBranch) > 0 {
		sim.blockchain.reorg()
		//sim.setState(sim.state - 2)
		sim.setState(0)
	}
}

//smWinsRace occurs when SM has a one block lead, and HM finds a block and publishes it.
//SM then publishes her block, and gamma has led to SM block winning
func (sim *Simulation) smWinsRace(delay int) {
	log.Debug("smWinsRace")
	sim.setRealTime(delay)
	sim.newPrivateBlock()
	//sim.blockchain.reorgRace()
	sim.blockchain.reorg()
	sim.setState(0)
}

//hmOnSm occurs when there is a race, such as in smWinsRace(), but the HM
//mines a new block that mines on top of the SM block.
func (sim *Simulation) hmOnSm(delay int) {
	log.Debug("hmOnSm")
	sim.setRealTime(delay)
	// FIXME: reorg() sets nextDif = privDif, but that difficulty change ought to have influenced whether we reach this branch in the first place.
	// Really, the notion of gamma would need to be changed to reflect different DAAs, but we assume that away here.
	sim.blockchain.reorg()
	sim.blockchain.newBlock(sim.realTime)
	sim.blockchain.nextPrivateDifficulty = sim.blockchain.nextDifficulty
	sim.blockchain.setForkHeight(0)
	sim.setState(0)
	/*
		fmt.Printf("new chain\n")
		fmt.Println(sim.blockchain.chain[sim.blockchain.height-4:])
	*/
}

//hmWinsRace occurs when SM had a one block lead, HM mines the next block and SM
//publishes her block but loses the race
func (sim *Simulation) hmWinsRace(delay int) {
	log.Debug("hwWinsRace")
	sim.setRealTime(delay)
	sim.blockchain.newBlock(sim.realTime)
	for len(sim.blockchain.privateBranch) > 0 {
		sim.blockchain.popFromPrivateChain()
	}
	sim.blockchain.nextPrivateDifficulty = sim.blockchain.nextDifficulty
	sim.blockchain.setForkHeight(0)
	sim.setState(0)
}

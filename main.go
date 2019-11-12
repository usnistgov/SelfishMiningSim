package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	color "github.com/logrusorgru/aurora"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/rand"
	yaml "gopkg.in/yaml.v2"
)

var algoMap = map[string]Difficulty{
	"btc":  btcDifficulty{Period: 2016, OffByOne: true},
	"bch":  bchDifficulty{Lookback: 144, OffByOne: true, Mediantimepast: 3},
	"dash": dashDifficulty{NPastBlocks: 24, OffByOne: true},
	"xmr":  xmrDifficulty{Lookback: 720, Delay: 15, Outliers: 60},
	"zec": zecDifficulty{NAveragingInterval: 17, NMedianTimespan: 11, NMaxAdjustUp: 16,
		NMaxAdjustDown: 32, NPOWDampeningFactor: 4.0},
}

//SimulationAvgResults contains the average reults for numsims runs of the simulation for the given params
type SimulationAvgResults struct {
	NumSims                int     `json:"numsims"`
	Alpha                  float64 `json:"alpha"`
	Gamma                  float64 `json:"gamma"`
	Timewarp               int     `json:"timewarp"`
	Numblocks              int     `json:"numblocks"`
	Blocktime              int     `json:"blocktime"`
	WinRatio               float64 `json:"winratio"`
	AdjustedWinning        float64 `json:"adjustedwinning"`
	SelfishSecondsPerBlock float64 `json:"selfishsecondsperblock"`
	RelativeGain           float64 `json:"relativegain"`
	AdjustedRelativeGain   float64 `json:"adjustedrelativegain"`
	GainStdDev             float64 `json:"gainstddev"`
	AdjustedGainStdDev     float64 `json:"adjustedgainsteddev"`
	SecondsPerBlockStdDev  float64 `json:"secondsperblockstddev"`
	FinalHeight            float64 `json:"finalheight"`
	NumReorgs              float64 `json:"numreorgs"`
	SmWinReorgs            float64 `json:"smwinreorgs"`
	DidBetterNaive         float64 `json:"didbetternaive"`
	DidBetterTimeAdjust    float64 `json:"didbettertimeadjust"`
}

//AllResults encompases all results for this program execution
type AllResults struct {
	Daa     string                 `json:"daa"`
	Params  Difficulty             `json:"difficulty_parameters"`
	Results []SimulationAvgResults `json:"results"`
}

var results AllResults
var resultFile *os.File
var appending bool
var resultFileName = "results.json"

func main() {

	if _, err := os.Stat(resultFileName); err == nil {
		appending = true
	} else {
		appending = false
	}
	//resultFile, err = os.OpenFile(resultFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)

	var numSims, numBlocks, timewarp, blockTime int
	var alpha, gamma float64
	var daa, logLevel string

	//Variables if we are adjusting parameters across different simulations
	var timewarpMax, timewarpStep int
	var alphaMax, alphaStep, gammaMax, gammaStep float64

	flag.StringVar(&daa, "algo", "", "REQUIRED Difficulty algorithm to use. Options: BTC, BCH, ZEC, XMR, DASH")

	flag.IntVar(&numSims, "numsims", 1, "Number of simulations to run. If we are over a range, this is the number of sims per permutation of parameters.")
	flag.IntVar(&numBlocks, "numblocks", 5000, "Number of blocks to simulate per simulation")
	flag.IntVar(&timewarp, "timewarp", 0, "Number of seconds to timewarp ahead. Lower bound if we are going over a range")
	flag.IntVar(&blockTime, "blocktime", -1, "Time between blocks. Default for the chosen algorithm if unspecified")

	flag.Float64Var(&alpha, "alpha", 0.35, "Proportion of the network hashrated controlled by the selfish miner. Lower bound if we are going over a range")
	flag.Float64Var(&gamma, "gamma", 0.0, "Portion of the network that mines on selfish miner blocks during a race/fork. Lower bound if we are going over a range")
	flag.Float64Var(&alphaMax, "alphamax", 0.0, "Max alpha if we are iterating over a range of alphas")
	flag.Float64Var(&alphaStep, "alphastep", 0.01, "How much to increment alpha per iteration")
	flag.Float64Var(&gammaMax, "gammamax", 0.0, "Max gamma if we are iterating over a range of gamma")
	flag.Float64Var(&gammaStep, "gammastep", 0.01, "How much to increment gamma per iteration")

	flag.IntVar(&timewarpMax, "timewarpmax", 0, "Max timewarp if we are iterating over a range")
	flag.IntVar(&timewarpStep, "timewarpstep", 1, "How much to increment timewarp per iteration")

	flag.StringVar(&logLevel, "loglevel", "warn", "Logging level. Options: Debug, Info, Warn, Error. If invalid given, fallback to warn")

	flag.Parse()

	daa = strings.ToLower(daa)

	if _, ok := algoMap[daa]; !ok {
		flag.Usage()
		log.Fatal("Attempted to use invalid diff algo")
	}

	if numSims < 1 || numBlocks < 1 || timewarp < 0 || timewarp > 7200 || alpha < 0.01 || alpha > 1.0 || gamma < 0.0 || gamma > 1.0 || blockTime < -1 || blockTime == 0 {
		flag.Usage()
		log.Fatal("Attempted to use invalid simulation parameters")
	}

	if alphaMax < 0.0 || gammaMax < 0.0 || (alphaMax > 0.0 && alphaMax <= alpha) || (gammaMax > 0.0 && gammaMax <= gamma) || alphaStep <= 0.0 || gammaStep <= 0.0 {
		flag.Usage()
		log.Fatal("Attempted to use invalid iteration parameters for alpha or gamma")
	}

	if timewarpMax < 0 || timewarpMax > 7200 || (timewarpMax > 0 && timewarpMax <= timewarp) {
		flag.Usage()
		log.Fatal("Attempted to use invalid iteration parameters for timewarp")
	}

	if blockTime == -1 {
		switch daa {
		case "btc", "bch":
			blockTime = 600
		case "dash", "zec":
			blockTime = 150
		case "xmr":
			blockTime = 120
		}
	}

	if alphaMax == 0.0 {
		alphaMax = alpha
	}
	if gammaMax == 0.0 {
		gammaMax = gamma
	}
	if timewarpMax == 0.0 {
		timewarpMax = timewarp
	}

	logLevel = strings.ToLower(logLevel)
	if logLevel == "debug" {
		log.SetLevel(log.DebugLevel)
	} else if logLevel == "info" {
		log.SetLevel(log.InfoLevel)
	} else if logLevel == "error" {
		log.SetLevel(log.ErrorLevel)
	} else {
		log.SetLevel(log.WarnLevel)
	}

	diffAlgo := loadYamlFile(daa)

	results.Daa = daa
	results.Params = diffAlgo

	simuationResults := make([]SimulationResult, numSims)
	selfishSecondsPerBlockHistory := make([]float64, numSims)
	gainHistory := make([]float64, numSims)
	adjustedGainHistory := make([]float64, numSims)
	resultChannel := make(chan SimulationResult, numSims)

	//Need to round these variables due to floating point errors
	alpha = toFixed(alpha, 3)
	alphaMax = toFixed(alphaMax, 3)
	alphaStep = toFixed(alphaStep, 3)
	gamma = toFixed(gamma, 3)
	gammaMax = toFixed(gammaMax, 3)
	gammaStep = toFixed(gammaStep, 3)

	timeStart := time.Now()

	//Ensure we save the results for unexpected shutdown
	defer saveResults()
	sigChannel := make(chan os.Signal)
	signal.Notify(sigChannel, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChannel
		saveResults()
		os.Exit(1)
	}()

	fmt.Println("Simulating with the following parameters")
	fmt.Printf("Algo: %s\tNumber of blocks: %d\tNumber of sims: %d\n", daa, numBlocks, numSims)
	fmt.Printf("Params: %s\n", results.Params)
	fmt.Printf("Alpha range:\t%f - %f (step: %f)\n", color.Green(alpha), color.Green(alphaMax), color.Green(alphaStep))
	fmt.Printf("Gamma range:\t%f - %f (step: %f)\n", color.Cyan(gamma), color.Cyan(gammaMax), color.Cyan(gammaStep))
	fmt.Printf("TImewarp range:\t%d -  %d (step: %d)\n\n", color.Magenta(timewarp), color.Magenta(timewarpMax), color.Magenta(timewarpStep))
	for alphaT := alpha; alphaT <= alphaMax; alphaT = toFixed(alphaT+alphaStep, 3) {
		for gammaT := gamma; gammaT <= gammaMax; gammaT = toFixed(gammaT+gammaStep, 3) {
			for timewarpT := timewarp; timewarpT <= timewarpMax; timewarpT += timewarpStep {
				fmt.Printf("Simulating: Alpha: %f\tGamma: %f\tTimewarp: %d", color.Green(alphaT), color.Cyan(gammaT), color.Magenta(timewarpT))
				simTime := time.Now()
				avgSimResults := SimulationAvgResults{
					NumSims:   numSims,
					Alpha:     alphaT,
					Gamma:     gammaT,
					Timewarp:  timewarpT,
					Numblocks: numBlocks,
					Blocktime: blockTime,
				}
				for i := 0; i < numSims; i++ {
					var sim Simulation
					sim.init(alphaT, gammaT, numBlocks, timewarpT, false, diffAlgo, blockTime, rand.Int())
					alpha = sim.alpha
					go sim.runSimulation(resultChannel)
				}

				var winRatioTotal, adjustedWinningTotal, selfishSecondsPerBlockTotal, numReorgsTotal, smReorgWinTotal float64
				var relativeGainAvg, adjustedRelativeGainAvg float64
				var didBetterNaive, didBetter float64
				var finalHeight int
				for i := 0; i < numSims; i++ {
					res := <-resultChannel
					//simuationResults[i] <- resultChannel
					simuationResults[i] = res
					selfishSecondsPerBlockHistory[i] = res.SelfishSecondsPerBlock
					gainHistory[i] = res.RelativeGain
					adjustedGainHistory[i] = res.AdjustedRelativeGain

					if res.WinRatio > alpha {
						didBetterNaive++
					}
					if res.AdjustedWinning > alpha {
						didBetter++
					}

					relativeGainAvg += res.RelativeGain
					adjustedRelativeGainAvg += res.AdjustedRelativeGain
					winRatioTotal += res.WinRatio
					adjustedWinningTotal += res.AdjustedWinning
					selfishSecondsPerBlockTotal += res.SelfishSecondsPerBlock
					numReorgsTotal += float64(res.NumReorgs)
					smReorgWinTotal += float64(res.SmWinReorgs) / float64(res.NumReorgs)
					finalHeight += res.FinalHeight
					//fmt.Printf("Finished simulation with height %d\n", res.FinalHeight)
				}

				avgSimResults.WinRatio = winRatioTotal / float64(numSims)
				avgSimResults.AdjustedWinning = adjustedWinningTotal / float64(numSims)
				avgSimResults.SelfishSecondsPerBlock = selfishSecondsPerBlockTotal / float64(numSims)
				avgSimResults.NumReorgs = numReorgsTotal / float64(numSims)
				avgSimResults.SmWinReorgs = smReorgWinTotal / float64(numSims)
				avgSimResults.FinalHeight = float64(finalHeight) / float64(numSims)
				avgSimResults.RelativeGain = relativeGainAvg / float64(numSims)
				avgSimResults.AdjustedRelativeGain = adjustedRelativeGainAvg / float64(numSims)

				avgSimResults.DidBetterNaive = didBetterNaive / float64(numSims)
				avgSimResults.DidBetterTimeAdjust = didBetter / float64(numSims)

				avgSimResults.GainStdDev = calcStdDev(gainHistory)
				avgSimResults.AdjustedGainStdDev = calcStdDev(adjustedGainHistory)
				avgSimResults.SecondsPerBlockStdDev = calcStdDev(selfishSecondsPerBlockHistory)

				results.Results = append(results.Results, avgSimResults)
				fmt.Printf("\t(%s) ", time.Since(simTime))
				fmt.Printf("Std dev: %f\n", calcStdDev(gainHistory))
			}
		}
	}

	fmt.Printf("Total running time: %s\n", time.Since(timeStart))
	fmt.Printf("Finished at : %s ", time.Now())
}

func calcStdDev(inputs []float64) float64 {
	var rounds = len(inputs)
	var total = sum(inputs...)
	var xbar = total / float64(rounds)
	var sumSquaredDiff = 0.0
	for i := 0; i < rounds; i++ {
		var diff = inputs[i] - xbar
		var squaredDiff = diff * diff
		sumSquaredDiff += squaredDiff
	}
	var tmp = sumSquaredDiff / float64(rounds)
	var stdDev = math.Sqrt(tmp)
	return stdDev
}

func createYamlFiles() {
	for algo, diff := range algoMap {
		fileName := algo + ".yaml"
		f, _ := os.Create(fileName)
		y, _ := yaml.Marshal(diff)
		f.Write(y)
		f.Close()
	}
}

func loadYamlFile(algo string) Difficulty {
	fileName := algo + ".yaml"
	f, _ := os.Open(fileName)
	d := yaml.NewDecoder(bufio.NewReader(f))
	switch algo {
	case "bch":
		var temp bchDifficulty
		d.Decode(&temp)
		return temp
	case "btc":
		var temp btcDifficulty
		d.Decode(&temp)
		return temp
	case "dash":
		var temp dashDifficulty
		d.Decode(&temp)
		return temp
	case "xmr":
		var temp xmrDifficulty
		d.Decode(&temp)
		return temp
	case "zec":
		var temp zecDifficulty
		d.Decode(&temp)
		return temp
	}

	return nil
}

func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

func toFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}

func saveResults() {
	resultFile, err := os.OpenFile(resultFileName, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.WithField("Error", err).Fatal("Failed to open results file")
	}
	fmt.Println("Writing results to JSON file")
	var jsonResults []byte
	if !appending {
		var tmp []AllResults
		tmp = append(tmp, results)
		jsonResults, _ = json.MarshalIndent(tmp, "", "\t")
	} else {
		jsonResults, _ = json.MarshalIndent(results, "", "\t")
		resultFile.Seek(-1, 2)
		resultFile.WriteString(",")
	}
	resultFile.Write(jsonResults)
	if appending {
		resultFile.WriteString("]")
	}
	resultFile.Close()
}

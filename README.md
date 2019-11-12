# Selfish Mining Simulator

Program to simulate running a selfish mining attack (with optional timestamp manipulation) on a blockchain for different difficulty adjustment algorithms. Metrics from the simulation are saved for later analysis. Program should run on all platforms that go supports. The code is not as efficient as it could be, however it does run concurrently, so the more cores the better.

Code is currently in a **beta** state. This code needs to be reword to be more efficient and integrate lessons learned along the way:)

For questions please contact either Tyler Diamond (tyler.diamond@nist.gov) or Michael Davidson (michael.davidson@nist.gov).

Usage:
 |   Parameter   |   Type   |   Description   |
 |:-------------:|:---------|-----------------|
 | algo | float |  REQUIRED Difficulty algorithm to use. Options: BTC, BCH, ZEC, XMR, DASH |
 | alpha | float | Proportion of the network hashrated controlled by the selfish miner. Lower bound if we are going over a range (default 0.35) |
 | alphamax | float | Max alpha if we are iterating over a range of alphas |
 | alphastep | float |  How much to increment alpha per iteration (default 0.01) |
 | blocktime | int | Time between blocks. Default for the chosen algorithm if unspecified (default -1) |
 | gamma | int | Portion of the network that mines on selfish miner blocks during a race/fork. Lower bound if we are going over a range |
 | gammamax | float | Max gamma if we are iterating over a range of gamma |
 | gammastep | float |  How much to increment gamma per iteration (default 0.01) |
 | timewarp | int | Number of seconds to timewarp ahead. Lower bound if we are going over a range |
 | timewarpmax | int | Max timewarp if we are iterating over a range
 | timewarpstep | int | How much to increment timewarp per iteration (default 1) |
  | loglevel | string | Logging level. Options: Debug, Info, Warn, Error. If invalid given, fallback to warn (default "warn") |
  | numblocks | int | Number of blocks to simulate per simulation (default 5000) |
  | numsims | int |  Number of simulations to run. If we are over a range, this is the number of sims per permutation of parameters. (default 1) |
       
## TODO:
- Ensure integrity of data according to NIST guidelines (https://inet.nist.gov/adlp/directives/managing-public-access-results-federally-funded-research-0 )

## Integrity (sha256):
> 602a941d0980375bafa497e91fd5e77953dd6d6743d31de41fb47d02d2a32577  all_results.json

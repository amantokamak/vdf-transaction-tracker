package trackingService

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type VDFTracker struct {
	client        *ethclient.Client
	contractAddrs []common.Address
}

func NewVDFTracker() (*VDFTracker, error) {
	rpcEndpoint := os.Getenv("ETH_RPC_ENDPOINT")
	if rpcEndpoint == "" {
		return nil, errors.New("ETH_RPC_ENDPOINT environment variable is not set")
	}
	client, err := ethclient.Dial(rpcEndpoint)
	if err != nil {
		return nil, fmt.Errorf("error connecting to Ethereum client: %v", err)
	}

	contractAddrsEnv := os.Getenv("CONTRACT_ADDRESSES")
	if contractAddrsEnv == "" {
		return nil, errors.New("CONTRACT_ADDRESSES environment variable is not set")
	}

	var addrStrings []string
	err = json.Unmarshal([]byte(contractAddrsEnv), &addrStrings)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CONTRACT_ADDRESSES environment variable: %v", err)
	}

	contractAddrs := make([]common.Address, 0, len(addrStrings))
	for _, addr := range addrStrings {
		parsedAddr := common.HexToAddress(addr)
		if parsedAddr == (common.Address{}) {
			log.Printf("Invalid contract address: %s", addr)
			continue
		}
		contractAddrs = append(contractAddrs, parsedAddr)
	}

	if len(contractAddrs) == 0 {
		return nil, errors.New("no valid contract addresses found in CONTRACT_ADDRESSES environment variable")
	}

	return &VDFTracker{
		client:        client,
		contractAddrs: contractAddrs,
	}, nil
}

func (t *VDFTracker) TrackVDFEvents(ctx context.Context) error {
	eventCommitC := []byte("CommitC(uint256,uint256,address,bytes)")
	hashCommitC := crypto.Keccak256Hash(eventCommitC)

	eventRandomWordsRequested := []byte("RandomWordsRequested(uint256)")
	hashRandomWordsRequested := crypto.Keccak256Hash(eventRandomWordsRequested)

	eventRecovered := []byte("Recovered(uint256,address,bytes)")
	hashRecovered := crypto.Keccak256Hash(eventRecovered)

	eventFulfillRandomness := []byte("FulfillRandomness(uint256,bool,address)")
	hashFulfillRandomness := crypto.Keccak256Hash(eventFulfillRandomness)

	fromBlockStr := os.Getenv("FROM_BLOCK")
	if fromBlockStr == "" {
		return errors.New("FROM_BLOCK environment variable is not set")
	}
	fromBlockInt, err := strconv.ParseInt(fromBlockStr, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse FROM_BLOCK environment variable: %v", err)
	}
	startBlock := big.NewInt(fromBlockInt)

	header, err := t.client.HeaderByNumber(ctx, nil)
	if err != nil {
		log.Printf("Failed to get latest block header: %v\n", err)
		return err
	}
	latestBlock := header.Number

	query := ethereum.FilterQuery{
		FromBlock: startBlock,
		ToBlock:   latestBlock,
		Addresses: t.contractAddrs,
		Topics:    [][]common.Hash{{hashCommitC, hashRandomWordsRequested, hashRecovered, hashFulfillRandomness}},
	}

	historicalLogs, err := t.client.FilterLogs(ctx, query)
	if err != nil {
		log.Printf("Failed to fetch historical CommitC events: %v\n", err)
		return err
	}

	for _, delog := range historicalLogs {
		switch {
		case delog.Topics[0] == hashCommitC:
			err := t.processCommitCLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical CommitC event log: %v", err)
			}
		case delog.Topics[0] == hashRandomWordsRequested:
			err := t.processRandomWordsRequestedLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical RandomWordsRequested event log: %v", err)
			}
		case delog.Topics[0] == hashRecovered:
			err := t.processRecoveredLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical Recovered event log: %v", err)
			}
		case delog.Topics[0] == hashFulfillRandomness:
			err := t.processFulfillRandomnessLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical FulfillRandomness event log: %v", err)
			}
		default:
			fmt.Printf("Unrecognized event log with topic: %x", delog.Topics[0])
		}

	}

	interval := os.Getenv("FETCH_INTERVAL")
	if interval == "" {
		interval = "1s"
	}
	duration, err := time.ParseDuration(interval)
	if err != nil {
		log.Printf("Failed to parse FETCH_INTERVAL: %v, defaulting to 10 minutes\n", err)
		duration = 1 * time.Minute
	}

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.fetchNewLogs(ctx, hashCommitC, hashRandomWordsRequested, hashRecovered, hashFulfillRandomness, latestBlock)
		case <-ctx.Done():
			log.Printf("Context done, stopping event tracking")
			return ctx.Err()
		}
	}
}

func (t *VDFTracker) fetchNewLogs(ctx context.Context, hashCommitC common.Hash, hashRandomWordsRequested common.Hash, hashRecovered common.Hash, hashFulfillRandomness common.Hash, fromBlock *big.Int) {

	header, err := t.client.HeaderByNumber(ctx, nil)
	if err != nil {
		log.Printf("Failed to get latest block header: %v\n", err)
		return
	}
	latestBlock := header.Number

	query := ethereum.FilterQuery{
		FromBlock: fromBlock,
		ToBlock:   latestBlock,
		Addresses: t.contractAddrs,
		Topics:    [][]common.Hash{{hashCommitC, hashRandomWordsRequested, hashRecovered, hashFulfillRandomness}},
	}

	logs, err := t.client.FilterLogs(ctx, query)
	if err != nil {
		log.Printf("Failed to fetch new CommitC events: %v\n", err)
		return
	}

	for _, delog := range logs {
		switch {
		case delog.Topics[0] == hashCommitC:
			err := t.processCommitCLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical CommitC event log: %v", err)
			}
		case delog.Topics[0] == hashRandomWordsRequested:
			err := t.processRandomWordsRequestedLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical RandomWordsRequested event log: %v", err)
			}
		case delog.Topics[0] == hashRecovered:
			err := t.processRecoveredLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical Recovered event log: %v", err)
			}
		case delog.Topics[0] == hashFulfillRandomness:
			err := t.processFulfillRandomnessLog(ctx, delog)
			if err != nil {
				fmt.Printf("Failed to process historical FulfillRandomness event log: %v", err)
			}
		default:
			fmt.Printf("Unrecognized event log with topic: %x", delog.Topics[0])
		}

	}
	
}

func (t *VDFTracker) processCommitCLog(ctx context.Context, delog types.Log) error {
	round, commitIndex, committer, commits, err := decodeCommitCLog(delog)
	if err != nil {
		log.Printf("Failed to decode CommitC event log: %v", err)
		return fmt.Errorf("failed to decode CommitC event log: %v", err)
	}

	log.Printf("Processing CommitC event log: Round %s, CommitIndex %s, Committer %s, Commitments %v", round.String(), commitIndex.String(), committer.String(), commits)

	return nil
}

func (t *VDFTracker) processRandomWordsRequestedLog(ctx context.Context, delog types.Log) error {
	round, err := decodeRandomWordsRequestedLog(delog)
	if err != nil {
		log.Printf("Failed to decode Random World event log: %v", err)
		return fmt.Errorf("failed to decode Random World event log: %v", err)
	}

	log.Printf("Processing Random World event log: Round %s", round.String())

	return nil
}

func (t *VDFTracker) processRecoveredLog(ctx context.Context, delog types.Log) error {
	round, recoverer, omega, err := decodeRecoveredLog(delog)
	if err != nil {
		log.Printf("Failed to decode Recovered event log: %v", err)
		return fmt.Errorf("failed to decode Recovered event log: %v", err)
	}

	log.Printf("Processing Recovered event log: Round %s, Recoverer %s, Omega %s", round.String(), recoverer.Hex(), omega.String())

	return nil
}

func (t *VDFTracker) processFulfillRandomnessLog(ctx context.Context, delog types.Log) error {
	round, success, fulfiller, err := decodeFulfillRandomessLog(delog)
	if err != nil {
		log.Printf("Failed to decode Fulfill event log: %v", err)
		return fmt.Errorf("failed to decode Recovered event log: %v", err)
	}
	// Implement processing logic for FulfillRandomness event log
	log.Printf("Processing FulfillRandomness event log: Round %s, Success %s, Fulfiller %s", round.String(), success, fulfiller.Hex())
	return nil
}

func decodeCommitCLog(delog types.Log) (*big.Int, *big.Int, common.Address, []*big.Int, error) {
	eventABI := `[{
    "type": "event",
    "name": "CommitC",
    "inputs": [{
        "type": "uint256",
        "name": "round",
        "internalType": "uint256",
        "indexed": false
    }, {
        "type": "uint256",
        "name": "commitIndex",
        "internalType": "uint256",
        "indexed": false
    }, {
        "type": "address",
        "name": "committer",
        "internalType": "address",
        "indexed": false
    }, {
        "type": "bytes",
        "name": "commitVal",
        "internalType": "bytes",
        "indexed": false
    }],
    "anonymous": false
}]`

	contractABI, err := abi.JSON(strings.NewReader(eventABI))
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}

	var commitEvent struct {
		Round       *big.Int
		CommitIndex *big.Int
		Committer   common.Address
		CommitVal   []byte
	}

	err = contractABI.UnpackIntoInterface(&commitEvent, "CommitC", delog.Data)
	if err != nil {
		log.Printf("Failed to unpack CommitC event log data: %v", err)
		log.Printf("Event data length: %d, Data: %v", len(delog.Data), delog.Data)
		return nil, nil, common.Address{}, nil, err
	}

	var commits []*big.Int
	if len(commitEvent.CommitVal) > 0 {
		commitBigInt := new(big.Int).SetBytes(commitEvent.CommitVal)
		commits = append(commits, commitBigInt)
	}

	return commitEvent.Round, commitEvent.CommitIndex, commitEvent.Committer, commits, nil
}

func decodeRandomWordsRequestedLog(delog types.Log) (*big.Int, error) {
	eventABI := `[{
    "type": "event",
    "name": "RandomWordsRequested",
    "inputs": [{
        "type": "uint256",
        "name": "round",
        "internalType": "uint256",
        "indexed": false
    }],
    "anonymous": false
}]`

	contractABI, err := abi.JSON(strings.NewReader(eventABI))
	if err != nil {
		return nil, err
	}

	var requestEvent struct {
		Round *big.Int
	}

	err = contractABI.UnpackIntoInterface(&requestEvent, "RandomWordsRequested", delog.Data)
	if err != nil {
		return nil, err
	}

	return requestEvent.Round, nil
}

func decodeRecoveredLog(delog types.Log) (*big.Int, common.Address, *big.Int, error) {
	eventABI := `[{
		"type": "event",
		"name": "Recovered",
		"inputs": [{
			"type": "uint256",
			"name": "round",
			"internalType": "uint256",
			"indexed": false
		}, {
			"type": "address",
			"name": "recoverer",
			"internalType": "address",
			"indexed": false
		}, {
			"type": "bytes",
			"name": "omega",
			"internalType": "bytes",
			"indexed": false
		}],
		"anonymous": false
	}]`

	contractABI, err := abi.JSON(strings.NewReader(eventABI))
	if err != nil {
		return nil, common.Address{}, nil, err
	}

	var recoveredEvent struct {
		Round     *big.Int
		Recoverer common.Address
		Omega     []byte
	}

	err = contractABI.UnpackIntoInterface(&recoveredEvent, "Recovered", delog.Data)
	if err != nil {
		log.Printf("Failed to unpack Recovered event log data: %v", err)
		log.Printf("Event data length: %d, Data: %v", len(delog.Data), delog.Data)
		return nil, common.Address{}, nil, err
	}

	// Convert Omega from bytes to a big.Int
	omega := new(big.Int)
	omega.SetBytes(recoveredEvent.Omega)

	return recoveredEvent.Round, recoveredEvent.Recoverer, omega, nil
}

func decodeFulfillRandomessLog(delog types.Log) (*big.Int, bool, common.Address, error) {
	eventABI := `[{
    "type": "event",
    "name": "FulfillRandomness",
    "inputs": [{
        "type": "uint256",
        "name": "round",
        "internalType": "uint256",
        "indexed": false
    }, {
        "type": "bool",
        "name": "success",
        "internalType": "bool",
        "indexed": false
    }, {
        "type": "address",
        "name": "fulfiller",
        "internalType": "address",
        "indexed": false
    }],
    "anonymous": false
}]`

contractABI, err := abi.JSON(strings.NewReader(eventABI))
if err != nil {
	return nil, false, common.Address{}, err
}

var fulfilledEvent struct {
	Round     *big.Int
	Success     bool
	Fulfiller common.Address
}

err = contractABI.UnpackIntoInterface(&fulfilledEvent, "FulfillRandomness", delog.Data)
if err != nil {
	log.Printf("Failed to unpack Fulfilled event log data: %v", err)
	log.Printf("Event data length: %d, Data: %v", len(delog.Data), delog.Data)
	return nil, false, common.Address{}, err
}

return fulfilledEvent.Round, fulfilledEvent.Success,  fulfilledEvent.Fulfiller, nil
}

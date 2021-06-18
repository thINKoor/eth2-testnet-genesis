package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/protolambda/ztyp/codec"
	"github.com/protolambda/ztyp/tree"
	"github.com/protolambda/ztyp/view"
	"github.com/zilm13/zcli/util"
	"github.com/zilm13/zrnt/eth2/beacon/common"
	"github.com/zilm13/zrnt/eth2/beacon/merge"
	"os"
	"time"
)

type MergeGenesisCmd struct {
	util.SpecOptions     `ask:"."`
	Eth1Config           string `ask:"--eth1-config" help:"Path to config JSON for eth1"`
	MnemonicsSrcFilePath string `ask:"--mnemonics" help:"File with YAML of key sources"`
	StateOutputPath      string `ask:"--state-output" help:"Output path for state file"`
	TranchesDir          string `ask:"--tranches-dir" help:"Directory to dump lists of pubkeys of each tranche in"`
}

func (g *MergeGenesisCmd) Help() string {
	return "Create genesis state for post-merge beacon chain, from eth1 and eth2 configs"
}

func (g *MergeGenesisCmd) Default() {
	g.SpecOptions.Default()
	g.Eth1Config = "eth1_testnet.json"
	g.MnemonicsSrcFilePath = "mnemonics.yaml"
	g.StateOutputPath = "genesis.ssz"
	g.TranchesDir = "tranches"
}

func (g *MergeGenesisCmd) Run(ctx context.Context, args ...string) error {
	eth1Genesis, err := loadEth1GenesisConf(g.Eth1Config)
	if err != nil {
		return err
	}

	eth1Db := rawdb.NewMemoryDatabase()
	eth1GenesisBlock := eth1Genesis.ToBlock(eth1Db)

	spec, err := g.SpecOptions.Spec()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(g.TranchesDir, 0777); err != nil {
		return err
	}

	validators, err := loadValidatorKeys(spec, g.MnemonicsSrcFilePath, g.TranchesDir)
	if err != nil {
		return err
	}

	if uint64(len(validators)) < spec.MIN_GENESIS_ACTIVE_VALIDATOR_COUNT {
		fmt.Printf("WARNING: not enough validators for genesis. Key sources sum up to %d total. But need %d.\n", len(validators), spec.MIN_GENESIS_ACTIVE_VALIDATOR_COUNT)
	}

	eth1BlockHash := common.Root(eth1GenesisBlock.Hash())

	state := merge.NewBeaconStateView(spec)
	if err := setupState(spec, state,
		common.Timestamp(eth1Genesis.Timestamp),
		eth1BlockHash, validators); err != nil {
		return err
	}

	if err := state.SetLatestExecutionPayloadHeader(&common.ExecutionPayloadHeader{
		BlockHash:   eth1BlockHash,
		ParentHash:  common.Root(eth1GenesisBlock.ParentHash()),
		CoinBase:    common.Eth1Address(eth1GenesisBlock.Coinbase()),
		StateRoot:   common.Bytes32(eth1GenesisBlock.Root()),
		Number:      view.Uint64View(eth1GenesisBlock.NumberU64()),
		GasLimit:    view.Uint64View(eth1GenesisBlock.GasLimit()),
		GasUsed:     view.Uint64View(eth1GenesisBlock.GasUsed()),
		Timestamp:   common.Timestamp(eth1GenesisBlock.Time()),
		ReceiptRoot: common.Bytes32(eth1GenesisBlock.ReceiptHash()),
		LogsBloom:   common.LogsBloom(eth1GenesisBlock.Bloom()),
		// empty transactions root
		TransactionsRoot: common.PayloadTransactionsType.DefaultNode().MerkleRoot(tree.GetHashFn()),
	}); err != nil {
		return err
	}

	t, err := state.GenesisTime()
	if err != nil {
		return err
	}
	fmt.Printf("eth2 genesis at %d + %d = %d  (%s)\n", eth1Genesis.Timestamp, spec.GENESIS_DELAY, t, time.Unix(int64(t), 0).String())

	fmt.Println("done preparing state, serializing SSZ now...")
	f, err := os.OpenFile(g.StateOutputPath, os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := bufio.NewWriter(f)
	defer buf.Flush()
	w := codec.NewEncodingWriter(f)
	if err := state.Serialize(w); err != nil {
		return err
	}
	fmt.Println("done!")
	return nil
}

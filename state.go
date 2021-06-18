package main

import (
	"fmt"
	"github.com/protolambda/ztyp/tree"
	"github.com/zilm13/zrnt/eth2/beacon/altair"
	"github.com/zilm13/zrnt/eth2/beacon/common"
	"github.com/zilm13/zrnt/eth2/beacon/merge"
	"github.com/zilm13/zrnt/eth2/beacon/phase0"
)

func setupState(spec *common.Spec, state common.BeaconState, eth1Time common.Timestamp,
	eth1BlockHash common.Root, validators []phase0.KickstartValidatorData) error {

	if err := state.SetGenesisTime(eth1Time + spec.GENESIS_DELAY); err != nil {
		return err
	}
	var forkVersion common.Version
	switch state.(type) {
	case *merge.BeaconStateView:
		forkVersion = spec.MERGE_FORK_VERSION
	case *altair.BeaconStateView:
		forkVersion = spec.ALTAIR_FORK_VERSION
	default:
		forkVersion = spec.GENESIS_FORK_VERSION
	}
	if err := state.SetFork(common.Fork{
		PreviousVersion: spec.GENESIS_FORK_VERSION,
		CurrentVersion:  forkVersion,
		Epoch:           common.GENESIS_EPOCH,
	}); err != nil {
		return err
	}
	// Empty deposit-tree
	eth1Dat := common.Eth1Data{
		DepositRoot:  phase0.NewDepositRootsView().HashTreeRoot(tree.GetHashFn()),
		DepositCount: 0,
		BlockHash:    eth1BlockHash,
	}
	if err := state.SetEth1Data(eth1Dat); err != nil {
		return err
	}
	// Leave the deposit index to 0. No deposits happened.
	if i, err := state.DepositIndex(); err != nil {
		return err
	} else if i != 0 {
		return fmt.Errorf("expected 0 deposit index in state, got %d", i)
	}
	var emptyBody tree.HTR
	switch state.(type) {
	case *merge.BeaconStateView:
		emptyBody = spec.Wrap(new(merge.BeaconBlockBody))
	case *altair.BeaconStateView:
		emptyBody = spec.Wrap(new(altair.BeaconBlockBody))
	default:
		emptyBody = spec.Wrap(new(phase0.BeaconBlockBody))
	}
	latestHeader := &common.BeaconBlockHeader{
		BodyRoot: emptyBody.HashTreeRoot(tree.GetHashFn()),
	}
	if err := state.SetLatestBlockHeader(latestHeader); err != nil {
		return err
	}
	// Seed RANDAO with Eth1 entropy
	err := state.SeedRandao(spec, eth1BlockHash)
	if err != nil {
		return err
	}

	for _, v := range validators {
		if err := state.AddValidator(spec, v.Pubkey, v.WithdrawalCredentials, v.Balance); err != nil {
			return err
		}
	}
	vals, err := state.Validators()
	if err != nil {
		return err
	}
	// Process activations
	for i := 0; i < len(validators); i++ {
		val, err := vals.Validator(common.ValidatorIndex(i))
		if err != nil {
			return err
		}
		vEff, err := val.EffectiveBalance()
		if err != nil {
			return err
		}
		if vEff == spec.MAX_EFFECTIVE_BALANCE {
			if err := val.SetActivationEligibilityEpoch(common.GENESIS_EPOCH); err != nil {
				return err
			}
			if err := val.SetActivationEpoch(common.GENESIS_EPOCH); err != nil {
				return err
			}
		}
	}
	if err := state.SetGenesisValidatorsRoot(vals.HashTreeRoot(tree.GetHashFn())); err != nil {
		return err
	}
	if st, ok := state.(*altair.BeaconStateView); ok {
		indicesBounded, err := common.LoadBoundedIndices(vals)
		if err != nil {
			return err
		}
		active := common.ActiveIndices(indicesBounded, common.GENESIS_EPOCH)
		indices, err := common.ComputeSyncCommitteeIndices(spec, state, common.GENESIS_EPOCH, active)
		if err != nil {
			return fmt.Errorf("failed to compute sync committee indices: %v", err)
		}
		pubs, err := common.NewPubkeyCache(vals)
		if err != nil {
			return err
		}
		// Note: A duplicate committee is assigned for the current and next committee at genesis
		syncCommittee, err := common.IndicesToSyncCommittee(indices, pubs)
		if err != nil {
			return err
		}
		syncCommitteeView, err := syncCommittee.View(spec)
		if err != nil {
			return err
		}
		if err := st.SetCurrentSyncCommittee(syncCommitteeView); err != nil {
			return err
		}
		if err := st.SetNextSyncCommittee(syncCommitteeView); err != nil {
			return err
		}
	}
	return nil
}

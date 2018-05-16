/*

  Copyright 2017 Loopring Project Ltd (Loopring Foundation).

  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.

*/

package accountmanager

import (
	"errors"
	rcache "github.com/Loopring/relay-lib/cache"
	"github.com/Loopring/relay-lib/eth/loopringaccessor"
	"github.com/Loopring/relay-lib/eventemitter"
	"github.com/Loopring/relay-lib/log"

	"fmt"
	"github.com/Loopring/relay-lib/marketutil"
	"github.com/Loopring/relay-lib/types"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"github.com/Loopring/relay-lib/zklock"
)

const (
	ZK_ACCOUNT_MANAGER = "account_manager"
)
type AccountManager struct {
	cacheDuration int64
	//maxBlockLength uint64
	block *ChangedOfBlock
}

func isPackegeReady() error {
	if !log.IsInit() {
		return fmt.Errorf("log must be init first")
	}
	if !rcache.IsInit() || !loopringaccessor.IsInit() || !marketutil.IsInit() || !zklock.IsInit() {
		return fmt.Errorf("cache、loopringaccessor、 marketutil and zklock must be init first")
	}
	return nil
}

func Initialize(options *AccountManagerOptions) AccountManager {
	if err := isPackegeReady(); nil != err {
		log.Fatalf(err.Error())
	}

	accountManager := AccountManager{}
	if options.CacheDuration > 0 {
		accountManager.cacheDuration = options.CacheDuration
	} else {
		accountManager.cacheDuration = 3600 * 24 * 100
	}
	//accountManager.maxBlockLength = 3000
	b := &ChangedOfBlock{}
	b.cachedDuration = big.NewInt(int64(500))
	accountManager.block = b
	accManager = &accountManager
	return accountManager
}

func (accountManager *AccountManager) Start() {
	log.Infof("accountmanger try to get zklock")
	zklock.TryLock(ZK_ACCOUNT_MANAGER)
	log.Infof("accountmanger has got zklock")
	transferWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleTokenTransfer}
	approveWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleApprove}
	wethDepositWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleWethDeposit}
	wethWithdrawalWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleWethWithdrawal}
	blockForkWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleBlockFork}
	blockEndWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleBlockEnd}
	blockNewWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleBlockNew}
	ethTransferWatcher := &eventemitter.Watcher{Concurrent: false, Handle: accountManager.handleEthTransfer}
	eventemitter.On(eventemitter.Transfer, transferWatcher)
	eventemitter.On(eventemitter.Approve, approveWatcher)
	eventemitter.On(eventemitter.EthTransfer, ethTransferWatcher)
	eventemitter.On(eventemitter.Block_End, blockEndWatcher)
	eventemitter.On(eventemitter.Block_New, blockNewWatcher)
	eventemitter.On(eventemitter.WethDeposit, wethDepositWatcher)
	eventemitter.On(eventemitter.WethWithdrawal, wethWithdrawalWatcher)
	eventemitter.On(eventemitter.ChainForkDetected, blockForkWatcher)
}

func (a *AccountManager) handleTokenTransfer(input eventemitter.EventData) (err error) {
	event := input.(*types.TransferEvent)

	//log.Info("received transfer event...")

	if event == nil || event.Status != types.TX_STATUS_SUCCESS {
		log.Info("received wrong status event, drop it")
		return nil
	}

	//balance
	a.block.saveBalanceKey(event.Sender, event.Protocol)
	a.block.saveBalanceKey(event.From, types.NilAddress)
	a.block.saveBalanceKey(event.Receiver, event.Protocol)

	//allowance
	if spender, err := loopringaccessor.GetSpenderAddress(event.To); nil == err {
		log.Debugf("handleTokenTransfer allowance owner:%s", event.Sender.Hex(), event.Protocol.Hex(), spender.Hex())
		a.block.saveAllowanceKey(event.Sender, event.Protocol, spender)
	}

	return nil
}

func (a *AccountManager) handleApprove(input eventemitter.EventData) error {
	event := input.(*types.ApprovalEvent)
	log.Debugf("received approval event, %s, %s", event.Protocol.Hex(), event.Owner.Hex())
	if event == nil || event.Status != types.TX_STATUS_SUCCESS {
		log.Info("received wrong status event, drop it")
		return nil
	}

	a.block.saveAllowanceKey(event.Owner, event.Protocol, event.Spender)

	a.block.saveBalanceKey(event.Owner, types.NilAddress)

	return nil
}

func (a *AccountManager) handleWethDeposit(input eventemitter.EventData) (err error) {
	event := input.(*types.WethDepositEvent)
	if event == nil || event.Status != types.TX_STATUS_SUCCESS {
		log.Info("received wrong status event, drop it")
		return nil
	}
	a.block.saveBalanceKey(event.Dst, event.Protocol)
	a.block.saveBalanceKey(event.From, types.NilAddress)
	return
}

func (a *AccountManager) handleWethWithdrawal(input eventemitter.EventData) (err error) {
	event := input.(*types.WethWithdrawalEvent)
	if event == nil || event.Status != types.TX_STATUS_SUCCESS {
		log.Info("received wrong status event, drop it")
		return nil
	}

	a.block.saveBalanceKey(event.Src, event.Protocol)
	a.block.saveBalanceKey(event.From, types.NilAddress)

	return
}

func (a *AccountManager) handleBlockEnd(input eventemitter.EventData) error {
	event := input.(*types.BlockEvent)
	log.Debugf("handleBlockEndhandleBlockEndhandleBlockEnd:%s", event.BlockNumber.String())

	a.block.syncAndSaveBalances()
	a.block.syncAndSaveAllowances()

	removeExpiredBlock(a.block.currentBlockNumber, a.block.cachedDuration)

	return nil
}

func (a *AccountManager) handleBlockNew(input eventemitter.EventData) error {
	event := input.(*types.BlockEvent)
	log.Debugf("handleBlockNewhandleBlockNewhandleBlockNewhandleBlockNew:%s", event.BlockNumber.String())
	a.block.currentBlockNumber = new(big.Int).Set(event.BlockNumber)
	return nil
}

func (a *AccountManager) handleEthTransfer(input eventemitter.EventData) error {
	event := input.(*types.TransferEvent)
	a.block.saveBalanceKey(event.From, types.NilAddress)
	a.block.saveBalanceKey(event.To, types.NilAddress)
	return nil
}

func (a *AccountManager) UnlockedWallet(owner string) (err error) {
	if !common.IsHexAddress(owner) {
		return errors.New("owner isn't a valid hex-address")
	}

	//accountBalances := AccountBalances{}
	//accountBalances.Owner = common.HexToAddress(owner)
	//accountBalances.Balances = make(map[common.Address]Balance)
	//err = accountBalances.getOrSave(a.cacheDuration)
	rcache.Set(unlockCacheKey(common.HexToAddress(owner)), []byte("true"), a.cacheDuration)
	return
}

func (a *AccountManager) handleBlockFork(input eventemitter.EventData) (err error) {
	event := input.(*types.ForkedEvent)
	log.Infof("the eth network may be forked. flush all cache, detectedBlock:%s", event.DetectedBlock.String())

	i := new(big.Int).Set(event.DetectedBlock)
	for i.Cmp(event.ForkBlock) >= 0 {
		changedOfBlock := &ChangedOfBlock{}
		changedOfBlock.currentBlockNumber = i
		changedOfBlock.syncAndSaveBalances()
		changedOfBlock.syncAndSaveAllowances()
		i.Sub(i, big.NewInt(int64(1)))
	}

	return nil
}
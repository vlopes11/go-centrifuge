package ethereum

import (
	"context"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/config"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-errors/errors"
	logging "github.com/ipfs/go-log"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
)

const TransactionUnderpriced = "replacement transaction underpriced"
const NonceTooLow = "nonce too low"

var log = logging.Logger("geth-client")
var gc *GethClient
var gcInit sync.Once

// getDefaultContextTimeout retrieves the default duration before an Ethereum call context should time out
func getDefaultContextTimeout() time.Duration {
	return config.Config.GetEthereumContextWaitTimeout()
}

func DefaultWaitForTransactionMiningContext() (ctx context.Context) {
	toBeDone := time.Now().Add(getDefaultContextTimeout())
	ctx, _ = context.WithDeadline(context.TODO(), toBeDone)
	return
}

// Abstract the "ethereum client" out so we can more easily support other clients
// besides Geth (e.g. quorum)
// Also make it easier to mock tests
type EthereumClient interface {
	GetClient() *ethclient.Client
}

type GethClient struct {
	Client *ethclient.Client
	Host   *url.URL
}

func (gethClient GethClient) GetClient() *ethclient.Client {
	return gethClient.Client
}

// GetConnection returns the connection to the configured `ethereum.gethSocket`.
// Note that this is a singleton and is the same connection for the whole application.
func GetConnection() EthereumClient {
	gcInit.Do(func() {
		log.Info("Opening connection to Ethereum:", config.Config.GetEthereumNodeURL())
		u, err := url.Parse(config.Config.GetEthereumNodeURL())
		if err != nil {
			log.Fatalf("Failed to connect to parse ethereum.gethSocket URL: %v", err)
		}
		client, err := ethclient.Dial(u.String())
		if err != nil {
			log.Fatalf("Failed to connect to the Ethereum client [%s]: %v", u.String(), err)
		} else {
			gc = &GethClient{client, u}
		}
	})
	return gc
}

// GetGethTxOpts retrieves the geth transaction options for the given account name. The account name influences which configuration
// is used.
func GetGethTxOpts(accountName string) (*bind.TransactOpts, error) {
	account, err := config.Config.GetEthereumAccountMap(accountName)
	if err != nil {
		err = errors.Errorf("could not find configured ethereum key for account [%v]. please check your configuration.\n", accountName)
		log.Error(err.Error())
		return nil, err
	}

	authedTransactionOpts, err := bind.NewTransactor(strings.NewReader(account["key"]), account["password"])
	if err != nil {
		err = errors.Errorf("Failed to load key with error: %v", err)
		log.Error(err.Error())
		return nil, err
	} else {
		authedTransactionOpts.GasPrice = config.Config.GetEthereumGasPrice()
		authedTransactionOpts.GasLimit = config.Config.GetEthereumGasLimit()
		return authedTransactionOpts, nil
	}
}

/**
Blocking Function that sends transaction using reflection wrapped in a retrial block. It is based on the TransactionUnderpriced error,
meaning that a transaction is being attempted to run twice, and the logic is to override the existing one. As we have constant
gas prices that means that a concurrent transaction race condition event has happened.
- contractMethod: Contract Method that implements GenericEthereumAsset (usually autogenerated binding from abi)
- params: Arbitrary number of parameters that are passed to the function fname call
*/
func SubmitTransactionWithRetries(contractMethod interface{}, params ...interface{}) (tx *types.Transaction, err error) {
	done := false
	maxTries := config.Config.GetEthereumMaxRetries()
	current := 0
	var f reflect.Value
	var in []reflect.Value
	var result []reflect.Value
	f = reflect.ValueOf(contractMethod)
	for !done {
		if current >= maxTries {
			log.Error("Max Concurrent transaction tries reached")
			break
		}
		current += 1
		in = make([]reflect.Value, len(params))
		for k, param := range params {
			in[k] = reflect.ValueOf(param)
		}
		result = f.Call(in)
		tx = result[0].Interface().(*types.Transaction)
		err = nil
		if result[1].Interface() != nil {
			err = result[1].Interface().(error)
		}

		if err != nil {
			if (err.Error() == TransactionUnderpriced) || (err.Error() == NonceTooLow) {
				log.Warningf("Concurrent transaction identified, trying again [%d/%d]\n", current, maxTries)
				time.Sleep(config.Config.GetEthereumIntervalRetry())
			} else {
				done = true
			}
		} else {
			done = true
		}
	}

	return
}

func GetGethCallOpts() (auth *bind.CallOpts) {
	// Assuring that pending transactions are taken into account by go-ethereum when asking for things like
	// specific transactions and client's nonce
	return &bind.CallOpts{Pending: true}
}
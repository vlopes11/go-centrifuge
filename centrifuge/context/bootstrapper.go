package context

import (
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/anchors"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/bootstrapper"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/config"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/coredocument/repository"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/ethereum"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/identity"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/invoice/repository"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/purchaseorder/repository"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/queue"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/storage"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/version"
	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("context")

type MainBootstrapper struct {
	Bootstrappers []bootstrapper.Bootstrapper
}

func (m *MainBootstrapper) PopulateDefaultBootstrappers() {
	m.Bootstrappers = []bootstrapper.Bootstrapper{
		&version.Bootstrapper{},
		&config.Bootstrapper{},
		&storage.Bootstrapper{},
		&coredocumentrepository.Bootstrapper{},
		&invoicerepository.Bootstrapper{},
		&purchaseorderrepository.Bootstrapper{},
		&ethereum.Bootstrapper{},
		&anchors.Bootstrapper{},
		&identity.Bootstrapper{},
		&queue.Bootstrapper{},
	}
}

func (m *MainBootstrapper) Bootstrap(context map[string]interface{}) error {
	for _, b := range m.Bootstrappers {
		err := b.Bootstrap(context)
		if err != nil {
			log.Error("Error encountered while bootstrapping", err)
			return err
		}
	}
	return nil
}
package signatures

import (
	"fmt"
	"time"

	"errors"

	"github.com/CentrifugeInc/centrifuge-protobufs/gen/go/coredocument"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/config"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/identity"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/utils"
	"golang.org/x/crypto/ed25519"
)

func ValidateCentrifugeID(signature *coredocumentpb.Signature, centrifugeId identity.CentID) error {
	centIDSignature, err := identity.NewCentID(signature.EntityId)
	if err != nil {
		return err
	}

	if !centrifugeId.Equal(centIDSignature) {
		return errors.New("signature entity doesn't match provided centID")
	}

	return nil

}

// ValidateSignature verifies the signature on the document
func ValidateSignature(signature *coredocumentpb.Signature, message []byte) error {
	centID, err := identity.NewCentID(signature.EntityId)
	if err != nil {
		return err
	}

	err = identity.ValidateKey(centID, signature.PublicKey)
	if err != nil {
		return err
	}

	return verifySignature(signature.PublicKey, message, signature.Signature)
}

// verifySignature verifies the signature using ed25519
func verifySignature(pubKey, message, signature []byte) error {
	valid := ed25519.Verify(pubKey, message, signature)
	if !valid {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// Sign the document with the private key and return the signature along with the public key for the verification
// assumes that signing root for the document is generated
func Sign(idConfig *config.IdentityConfig, payload []byte) *coredocumentpb.Signature {
	signature := ed25519.Sign(idConfig.PrivateKey, payload)
	return &coredocumentpb.Signature{
		EntityId:  idConfig.ID,
		PublicKey: idConfig.PublicKey,
		Signature: signature,
		Timestamp: utils.ToTimestamp(time.Now().UTC()),
	}
}

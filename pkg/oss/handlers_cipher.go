package oss

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/cipher"
	"github.com/liyang/weave/pkg/oms"
)

// SetCipherDecryptor wires the cipher.Decryptor so the handler can serve the
// object-path CipherTextProperty decrypt endpoint. When nil, the route
// returns CipherDecryptorNotConfigured.
func (h *Handler) SetCipherDecryptor(dec cipher.Decryptor) {
	h.cipherDecryptor = dec
}

// decryptionResult is the Foundry OSv2 DecryptionResult wire shape returned
// by the CipherTextProperty decrypt endpoint.
type decryptionResult struct {
	Plaintext string `json:"plaintext"`
}

// DecryptCipherTextProperty handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/ciphertexts/{property}/decrypt.
func (h *Handler) DecryptCipherTextProperty(w http.ResponseWriter, r *http.Request) {
	if h.cipherDecryptor == nil {
		apierror.WriteJSON(w, apierror.NewInternal("CipherDecryptorNotConfigured", nil))
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	propertyName := chi.URLParam(r, "property")

	obj, err := h.svc.GetObject(r.Context(), GetObjectRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectNotFound", map[string]string{
				"objectType": objectType,
				"primaryKey": primaryKey,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	rawValue, ok := obj.Properties[propertyName]
	if !ok || rawValue == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("PropertyNotFound", map[string]string{
			"property": propertyName,
		}))
		return
	}
	ciphertext, ok := rawValue.(string)
	if !ok {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCipherTextProperty", map[string]string{
			"property": propertyName,
			"reason":   "ciphertext property value is not a string",
		}))
		return
	}

	plaintext, err := h.cipherDecryptor.Decrypt(r.Context(), ciphertext)
	if err != nil {
		if errors.Is(err, cipher.ErrInvalidCiphertext) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidCiphertext", map[string]string{
				"property": propertyName,
				"reason":   err.Error(),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CipherDecryptError", map[string]string{
			"message": err.Error(),
		}))
		return
	}

	writeJSONOK(w, decryptionResult{Plaintext: plaintext})
}

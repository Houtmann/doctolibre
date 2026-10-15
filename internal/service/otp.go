package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"doctolibre/internal/outbox"
)

// Paramètres du code de vérification (OTP) de la réservation en ligne.
const (
	OTPTTL         = 10 * time.Minute
	OTPMaxAttempts = 5
	// OTPPerHour borne les codes envoyés au même destinataire sur une heure glissante.
	OTPPerHour = 3

	otpDigits = 6
	// TemplateOTP est le template email (internal/notify/templates/email).
	TemplateOTP = "otp"

	channelSMS   = "sms"
	channelEmail = "email"
)

// Erreurs de vérification du code.
var (
	// ErrOTPWrong : code incorrect ; l'utilisateur peut réessayer (dans la limite des essais).
	ErrOTPWrong = errors.New("code de vérification incorrect")
	// ErrOTPExpired : code expiré, déjà utilisé, trop d'essais ou lié à d'autres données ;
	// il faut en demander un nouveau.
	ErrOTPExpired = errors.New("code de vérification expiré ou invalide")
	// ErrOTPRateLimited : trop de codes demandés pour ce destinataire.
	ErrOTPRateLimited = errors.New("trop de codes demandés")
)

// OTPSender remet un code de vérification à son destinataire. Il reçoit la
// transaction courante : l'envoi réel (SMS, SMTP) se fait hors transaction, par
// le worker de l'outbox. recipient est un numéro E.164 (channel "sms") ou une
// adresse email (channel "email"). Ni le code ni le destinataire ne doivent être
// journalisés.
type OTPSender interface {
	SendOTP(ctx context.Context, tx pgx.Tx, practiceID uuid.UUID, practiceName, channel, recipient, code string) error
}

// OutboxOTPSender enfile le code dans l'outbox (SMS ou email), dans la transaction métier.
type OutboxOTPSender struct{}

// SendOTP implémente OTPSender.
func (OutboxOTPSender) SendOTP(ctx context.Context, tx pgx.Tx, practiceID uuid.UUID, practiceName, channel, recipient, code string) error {
	m := outbox.Msg{PracticeID: practiceID, Recipient: recipient}
	var payload any
	switch channel {
	case channelSMS:
		m.Kind, m.Template = outbox.KindSMS, "otp"
		payload = map[string]any{
			"body": fmt.Sprintf("%s : votre code de vérification est %s (valable 10 min).", truncate(practiceName, 30), code),
			"otp":  true,
		}
	case channelEmail:
		m.Kind, m.Template = outbox.KindEmail, TemplateOTP
		payload = map[string]string{"code": code, "valid_for": "10 minutes", "practice_name": practiceName}
	default:
		return fmt.Errorf("canal OTP inconnu %q", channel)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("payload OTP: %w", err)
	}
	m.Payload = b
	return outbox.Enqueue(ctx, tx, m)
}

// newOTPCode tire un code à 6 chiffres avec crypto/rand.
func newOTPCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("tirage du code: %w", err)
	}
	return fmt.Sprintf("%0*d", otpDigits, n.Int64()), nil
}

// otpHash : le code est lié au challenge (sel = identifiant), jamais stocké en clair.
func otpHash(id uuid.UUID, code string) string {
	sum := sha256.Sum256([]byte(id.String() + ":" + code))
	return hex.EncodeToString(sum[:])
}

func otpMatches(stored string, id uuid.UUID, code string) bool {
	return subtle.ConstantTimeCompare([]byte(stored), []byte(otpHash(id, code))) == 1
}

func sha256Hex(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0x1f})
	}
	return hex.EncodeToString(h.Sum(nil))
}

package vpn

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ryanborg/mediarium/internal/crypto"
)

// StoredConfig is a saved VPN config ("multiple provider
// configs stored, one active at a time, switchable from Settings").
type StoredConfig struct {
	ID       int64
	Label    string
	Provider string
	Active   bool
	Config   Config
}

type Repo struct {
	db  *sql.DB
	box *crypto.Box
}

func NewRepo(db *sql.DB, box *crypto.Box) *Repo {
	return &Repo{db: db, box: box}
}

func (r *Repo) Create(label, provider string, cfg Config) (StoredConfig, error) {
	encPriv, err := r.box.Encrypt(cfg.PrivateKey)
	if err != nil {
		return StoredConfig{}, fmt.Errorf("encrypt private key: %w", err)
	}
	var encPSK any
	if cfg.PresharedKey != "" {
		enc, err := r.box.Encrypt(cfg.PresharedKey)
		if err != nil {
			return StoredConfig{}, fmt.Errorf("encrypt preshared key: %w", err)
		}
		encPSK = enc
	}
	allowedIPs := strings.Join(cfg.AllowedIPs, ",")
	localAddrs := strings.Join(cfg.LocalAddresses, ",")
	dns := strings.Join(cfg.DNS, ",")

	res, err := r.db.Exec(
		`INSERT INTO vpn_configs (label, provider, private_key_encrypted, peer_public_key, preshared_key_encrypted, endpoint, allowed_ips, local_addresses, dns)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		label, provider, encPriv, cfg.PeerPublicKey, encPSK, cfg.Endpoint, allowedIPs, localAddrs, dns,
	)
	if err != nil {
		return StoredConfig{}, fmt.Errorf("insert vpn config: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return StoredConfig{}, fmt.Errorf("get inserted vpn config id: %w", err)
	}
	return StoredConfig{ID: id, Label: label, Provider: provider, Config: cfg}, nil
}

func (r *Repo) List() ([]StoredConfig, error) {
	rows, err := r.db.Query(`SELECT id, label, provider, private_key_encrypted, peer_public_key, preshared_key_encrypted, endpoint, allowed_ips, local_addresses, dns, active FROM vpn_configs ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list vpn configs: %w", err)
	}
	defer rows.Close()

	var out []StoredConfig
	for rows.Next() {
		sc, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// Active returns the currently active config, if any.
func (r *Repo) Active() (StoredConfig, bool, error) {
	row := r.db.QueryRow(`SELECT id, label, provider, private_key_encrypted, peer_public_key, preshared_key_encrypted, endpoint, allowed_ips, local_addresses, dns, active FROM vpn_configs WHERE active = 1 LIMIT 1`)
	sc, err := r.scan(row)
	if err == sql.ErrNoRows {
		return StoredConfig{}, false, nil
	}
	if err != nil {
		return StoredConfig{}, false, err
	}
	return sc, true, nil
}

// SetActive marks id as the sole active config, deactivating any other.
func (r *Repo) SetActive(id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if _, err := tx.Exec(`UPDATE vpn_configs SET active = 0`); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear active configs: %w", err)
	}
	if _, err := tx.Exec(`UPDATE vpn_configs SET active = 1 WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("set active config %d: %w", id, err)
	}
	return tx.Commit()
}

func (r *Repo) Deactivate() error {
	_, err := r.db.Exec(`UPDATE vpn_configs SET active = 0`)
	if err != nil {
		return fmt.Errorf("deactivate vpn configs: %w", err)
	}
	return nil
}

func (r *Repo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM vpn_configs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete vpn config %d: %w", id, err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (r *Repo) scan(scanner rowScanner) (StoredConfig, error) {
	var (
		sc                                        StoredConfig
		encPriv, endpoint, allowedIPs, localAddrs string
		encPSK, dns                               sql.NullString
	)
	err := scanner.Scan(&sc.ID, &sc.Label, &sc.Provider, &encPriv, &sc.Config.PeerPublicKey, &encPSK, &endpoint, &allowedIPs, &localAddrs, &dns, &sc.Active)
	if err != nil {
		return StoredConfig{}, err
	}

	priv, err := r.box.Decrypt(encPriv)
	if err != nil {
		return StoredConfig{}, fmt.Errorf("decrypt private key for vpn config %d: %w", sc.ID, err)
	}
	sc.Config.PrivateKey = priv
	if encPSK.Valid && encPSK.String != "" {
		psk, err := r.box.Decrypt(encPSK.String)
		if err != nil {
			return StoredConfig{}, fmt.Errorf("decrypt preshared key for vpn config %d: %w", sc.ID, err)
		}
		sc.Config.PresharedKey = psk
	}
	sc.Config.Endpoint = endpoint
	sc.Config.AllowedIPs = splitNonEmpty(allowedIPs)
	sc.Config.LocalAddresses = splitNonEmpty(localAddrs)
	sc.Config.DNS = splitNonEmpty(dns.String)
	return sc, nil
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

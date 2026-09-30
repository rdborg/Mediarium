package auth

// SetHashCostForTest changes the bcrypt work factor for new hashes and returns
// a function that puts it back.
func SetHashCostForTest(cost int) (restore func()) {
	old := hashCost
	hashCost = cost
	return func() { hashCost = old }
}

// StoredHashForTest is the password hash stored for an account.
func (s *Service) StoredHashForTest(id int64) string {
	var h string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&h); err != nil {
		return ""
	}
	return h
}

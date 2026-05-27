package secrets

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"os"
	"sort"
	"strings"
)

const (
	KeyPassphraseEnv = "MESHBAN_KEY_PASSPHRASE"
	WebTokenEnv      = "MESHBAN_WEB_TOKEN"
)

type Manager struct {
	path string
	data map[string]string
}

func LoadOrCreate(path string) (*Manager, error) {
	manager := &Manager{
		path: path,
		data: make(map[string]string),
	}

	// Create an empty env file if it does not exist.
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte{}, 0600); err != nil {
				return nil, err
			}

			return manager, nil
		}

		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" {
			continue
		}

		manager.data[key] = value

		// Sync values from the env file into the current process environment.
		_ = os.Setenv(key, value)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return manager, nil
}

func (m *Manager) Get(key string) string {
	key = strings.TrimSpace(key)

	if value, ok := m.data[key]; ok {
		return value
	}

	return os.Getenv(key)
}

func (m *Manager) Set(key string, value string) error {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	if key == "" {
		return nil
	}

	m.data[key] = value

	// This only updates the current process environment.
	_ = os.Setenv(key, value)

	return m.save()
}

func (m *Manager) Delete(key string) error {
	key = strings.TrimSpace(key)

	delete(m.data, key)

	// This only updates the current process environment.
	_ = os.Unsetenv(key)

	return m.save()
}

func (m *Manager) EnsureRandom(key string, byteSize int) (string, error) {
	current := strings.TrimSpace(m.Get(key))
	if current != "" {
		return current, nil
	}

	generated, err := randomSecret(byteSize)
	if err != nil {
		return "", err
	}

	if err := m.Set(key, generated); err != nil {
		return "", err
	}

	return generated, nil
}

func (m *Manager) save() error {
	keys := make([]string, 0, len(m.data))

	for key := range m.data {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	var builder strings.Builder

	for _, key := range keys {
		value := strings.TrimSpace(m.data[key])
		if value == "" {
			continue
		}

		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(value)
		builder.WriteString("\n")
	}

	return os.WriteFile(m.path, []byte(builder.String()), 0600)
}

func randomSecret(byteSize int) (string, error) {
	raw := make([]byte, byteSize)

	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

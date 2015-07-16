package fsrepo

import (
	"os"
	"time"

	homedir "github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/mitchellh/go-homedir"
	"github.com/ipfs/go-ipfs/repo/config"
)

// BestKnownPath returns the best known fsrepo path. If the ENV override is
// present, this function returns that value. Otherwise, it returns the default
// repo path.
func BestKnownPath() (string, error) {
	ipfsPath := config.DefaultPathRoot
	if os.Getenv(config.EnvDir) != "" {
		ipfsPath = os.Getenv(config.EnvDir)
	}
	ipfsPath, err := homedir.Expand(ipfsPath)
	if err != nil {
		return "", err
	}
	return ipfsPath, nil
}

func Reflog(str string) error {
	reflogFile, err := homedir.Expand(config.DefaultPathRoot + "/logs/HEAD")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(reflogFile, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return err
	}

	defer f.Close()
	_, err = f.WriteString(time.Now().String() + " " + str + "\n")
	return err
}

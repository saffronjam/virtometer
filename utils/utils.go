package utils

import (
	"encoding/json"
	"fmt"
	"github.com/melbahja/goph"
	"log"
	"math/rand"
	"performance/pkg/app"
	"strings"
	"time"
)

// SshCommand executes an SshCommand command on the given VM
func SshCommand(ip string, commands []string) ([]string, error) {
	var client *goph.Client
	var err error

	for {
		client, err = goph.NewUnknown(app.Config.Azure.Username, ip, goph.Password(app.Config.Azure.Password))
		if err != nil {
			if strings.Contains(err.Error(), "ssh: handshake failed:") {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			return nil, err
		}

		break
	}
	defer func(client *goph.Client) {
		err := client.Close()
		if err != nil {
			log.Println(err)
		}
	}(client)

	var outAll []string

	for _, command := range commands {
		for {
			out, err := client.Run(command)
			if err != nil && strings.Contains(err.Error(), "ssh: handshake failed:") {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			if out != nil {
				outAll = append(outAll, string(out))
			}

			if err != nil {
				if len(outAll) > 0 {
					return outAll, fmt.Errorf("ssh err %w. details: %s", err, outAll[len(outAll)-1])
				} else {
					return outAll, fmt.Errorf("ssh err %w", err)
				}
			}

			break
		}
	}

	return outAll, nil
}

// SshUpload copies a file from the local machine to the remote machine
func SshUpload(ip string, localPath string, remotePath string) error {
	client, err := goph.NewUnknown(app.Config.Azure.Username, ip, goph.Password(app.Config.Azure.Password))
	if err != nil {
		return err
	}
	defer func(client *goph.Client) {
		err := client.Close()
		if err != nil {
			log.Println(err)
		}
	}(client)

	err = client.Upload(localPath, remotePath)
	if err != nil {
		return err
	}

	return nil
}

// SshDownload copies a file from the remote machine to the local machine
func SshDownload(ip string, remotePath string, localPath string) error {
	client, err := goph.NewUnknown(app.Config.Azure.Username, ip, goph.Password(app.Config.Azure.Password))
	if err != nil {
		return err
	}
	defer func(client *goph.Client) {
		err := client.Close()
		if err != nil {
			log.Println(err)
		}
	}(client)

	err = client.Download(remotePath, localPath)
	if err != nil {
		return err
	}

	return nil
}

// ParseSshOutput parses the string list's first string returned by SshCommand to a struct
func ParseSshOutput[T any](output []string) (*T, error) {
	if len(output) == 0 {
		return nil, nil
	}

	var parsedOutput T
	err := json.Unmarshal([]byte(output[0]), &parsedOutput)
	if err != nil {
		return nil, err
	}

	return &parsedOutput, nil
}

// RandomName generates a random name with the given prefix
func RandomName(prefix string) string {
	randomString := func(length int) string {
		letterBytes := "abcdefghijklmnopqrstuvwxyz"
		b := make([]byte, length)
		for i := range b {
			b[i] = letterBytes[rand.Intn(len(letterBytes))]
		}
		return string(b)
	}

	return prefix + "-" + randomString(5)
}

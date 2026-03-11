package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
)

func DirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", path)
	}

	return true, nil
}

func WriteJSON(path string, obj any) error {
	tmpfile := path + ".tmp"
	file, err := os.Create(tmpfile)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpfile)
	err = json.NewEncoder(file).Encode(obj)
	if err != nil {
		return fmt.Errorf("failed to encode list: %w", err)
	}

	return os.Rename(tmpfile, path)
}

func GitCloneOrPull(repository, directory string) error {
	exists, err := DirExists(directory)
	if err != nil {
		return err
	}

	if !exists {
		err = os.MkdirAll(directory, 0755)
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", directory, err)
		}
		git := exec.Command("git", "clone", repository, directory)
		if err := git.Run(); err != nil {
			os.RemoveAll(directory)
			return fmt.Errorf("git clone failed: %w", err)
		}
		return nil
	}

	git := exec.Command("git", "-C", directory, "pull")
	if err := git.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	return nil
}

func GitCloneTag(repository, tag, workdir string) (string, error) {

	parsedUrl, err := url.Parse(repository)
	parsedUrl.User = nil
	repolog := parsedUrl.String()

	sum := sha1.Sum([]byte(repolog + "|" + tag))
	name := hex.EncodeToString(sum[:])
	path := filepath.Join(workdir, name)
	ok, err := DirExists(path)
	if err != nil {
		return path, err
	}
	if ok {
		log.Printf("Skipping git clone for %s@%s", repolog, tag)
		return path, nil
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("can't create dir: %w", err)
	}

	log.Printf("Cloning %s@%s...", repolog, tag)
	git := exec.Command("git", "--no-advice", "clone", "--depth=1", "--branch", tag, repository, path)
	git.Env = append(git.Env, "GIT_TERMINAL_PROMPT=false")
	git.Stdout = os.Stdout
	git.Stderr = os.Stderr
	if err := git.Run(); err != nil {
		os.RemoveAll(path)
		return "", fmt.Errorf("git clone failed: %w", err)
	}

	return path, nil
}

package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func newProjectInDir(path string) error {
	// set path to be nested inside $GOPATH/src
	gopath := os.Getenv("GOPATH")
	path = filepath.Join(gopath, "src", path)

	// check if anything exists at the path, ask if it should be overwritten
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		fmt.Println("Path exists, overwrite contents? (y/N):")

		var answer string
		_, err := fmt.Scanf("%s\n", &answer)
		if err != nil {
			if err.Error() == "unexpected newline" {
				answer = ""
			} else {
				return err
			}
		}

		answer = strings.ToLower(answer)

		switch answer {
		case "n", "no", "\r\n", "\n", "":
			fmt.Println("")

		case "y", "yes":
			err := os.RemoveAll(path)
			if err != nil {
				return fmt.Errorf("Failed to overwrite %s. \n%s", path, err)
			}

			return createProjInDir(path)

		default:
			fmt.Println("Input not recognized. No files overwritten. Answer as 'y' or 'n' only.")
		}

		return nil
	}

	return createProjInDir(path)
}

var ponzuRepo = []string{"github.com", "bosssauce", "ponzu"}

func createProjInDir(path string) error {
	gopath := os.Getenv("GOPATH")
	repo := ponzuRepo
	local := filepath.Join(gopath, "src", filepath.Join(repo...))
	network := "https://" + strings.Join(repo, "/") + ".git"

	// create the directory or overwrite it
	err := os.MkdirAll(path, os.ModeDir|os.ModePerm)
	if err != nil {
		return err
	}

	if dev {
		if fork != "" {
			local = filepath.Join(gopath, "src", fork)
		}

		devClone := exec.Command("git", "clone", local, "--branch", "ponzu-dev", "--single-branch", path)
		devClone.Stdout = os.Stdout
		devClone.Stderr = os.Stderr

		err = devClone.Start()
		if err != nil {
			return err
		}

		err = devClone.Wait()
		if err != nil {
			return err
		}

		err = vendorCorePackages(path)
		if err != nil {
			return err
		}

		fmt.Println("Dev build cloned from " + local + ":ponzu-dev")
		return nil
	}

	// try to git clone the repository from the local machine's $GOPATH
	localClone := exec.Command("git", "clone", local, path)
	localClone.Stdout = os.Stdout
	localClone.Stderr = os.Stderr

	err = localClone.Start()
	if err != nil {
		return err
	}
	err = localClone.Wait()
	if err != nil {
		fmt.Println("Couldn't clone from", local, ". Trying network...")

		// try to git clone the repository over the network
		networkClone := exec.Command("git", "clone", network, path)
		networkClone.Stdout = os.Stdout
		networkClone.Stderr = os.Stderr

		err = networkClone.Start()
		if err != nil {
			fmt.Println("Network clone failed to start. Try again and make sure you have a network connection.")
			return err
		}
		err = networkClone.Wait()
		if err != nil {
			fmt.Println("Network clone failure.")
			// failed
			return fmt.Errorf("Failed to clone files from local machine [%s] and over the network [%s].\n%s", local, network, err)
		}
	}

	// create a 'vendor' directory in $path/cmd/ponzu and move 'content',
	// 'management' and 'system' packages into it
	err = vendorCorePackages(path)
	if err != nil {
		return err
	}

	gitDir := filepath.Join(path, ".git")
	err = os.RemoveAll(gitDir)
	if err != nil {
		fmt.Println("Failed to remove .git directory from your project path. Consider removing it manually.")
	}

	fmt.Println("New ponzu project created at", path)
	return nil
}

func vendorCorePackages(path string) error {
	vendorPath := filepath.Join(path, "cmd", "ponzu", "vendor", "github.com", "bosssauce", "ponzu")
	err := os.MkdirAll(vendorPath, os.ModeDir|os.ModePerm)
	if err != nil {
		// TODO: rollback, remove ponzu project from path
		return err
	}

	dirs := []string{"content", "management", "system"}
	for _, dir := range dirs {
		err = os.Rename(filepath.Join(path, dir), filepath.Join(vendorPath, dir))
		if err != nil {
			// TODO: rollback, remove ponzu project from path
			return err
		}
	}

	// create a user 'content' package
	contentPath := filepath.Join(path, "content")
	err = os.Mkdir(contentPath, os.ModeDir|os.ModePerm)
	if err != nil {
		// TODO: rollback, remove ponzu project from path
		return err
	}

	return nil
}

func buildPonzuServer(args []string) error {
	// copy all ./content .go files to $vendor/content
	// check to see if any file exists, move on to next file if so,
	// and report this conflict to user for them to fix & re-run build
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}

	contentSrcPath := filepath.Join(pwd, "content")
	contentDstPath := filepath.Join(pwd, "cmd", "ponzu", "vendor", "github.com", "bosssauce", "ponzu", "content")

	srcFiles, err := ioutil.ReadDir(contentSrcPath)
	if err != nil {
		return err
	}

	var conflictFiles = []string{"item.go", "types.go"}
	var mustRenameFiles = []string{}
	for _, srcFileInfo := range srcFiles {
		// check srcFile exists in contentDstPath
		for _, conflict := range conflictFiles {
			if srcFileInfo.Name() == conflict {
				mustRenameFiles = append(mustRenameFiles, conflict)
				continue
			}
		}

		dstFile, err := os.Create(filepath.Join(contentDstPath, srcFileInfo.Name()))
		if err != nil {
			return err
		}

		srcFile, err := os.Open(filepath.Join(contentSrcPath, srcFileInfo.Name()))
		if err != nil {
			return err
		}

		_, err = io.Copy(dstFile, srcFile)
		if err != nil {
			return err
		}
	}

	if len(mustRenameFiles) > 1 {
		fmt.Println("Ponzu couldn't fully build your project:")
		fmt.Println("Some of your files in the content directory exist in the vendored directory.")
		fmt.Println("You must rename the following files, as they conflict with Ponzu core:")
		for _, file := range mustRenameFiles {
			fmt.Println(file)
		}

		fmt.Println("Once the files above have been renamed, run '$ ponzu build' to retry.")
		return errors.New("Ponzu has very few internal conflicts, sorry for the inconvenience.")
	}

	// execute go build -o ponzu-cms cmd/ponzu/*.go
	mainPath := filepath.Join(pwd, "cmd", "ponzu", "main.go")
	optsPath := filepath.Join(pwd, "cmd", "ponzu", "options.go")
	build := exec.Command("go", "build", "-o", "ponzu-server", mainPath, optsPath)
	build.Stderr = os.Stderr
	build.Stdout = os.Stdout

	err = build.Start()
	if err != nil {
		return errors.New("Ponzu build step failed. Please try again. " + "\n" + err.Error())

	}
	err = build.Wait()
	if err != nil {
		return errors.New("Ponzu build step failed. Please try again. " + "\n" + err.Error())

	}

	return nil
}

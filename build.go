package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	// "path"
	"path/filepath"

	"github.com/creasty/defaults"
	"gopkg.in/yaml.v2"
)

type registry struct {
	UserVariable     string `yaml:"user_variable"`
	PasswordVariable string `yaml:"password_variable"`
}

func runExternalProgram(
	program string,
	args []string,
	env []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	_, _ = stdout.Write([]byte(fmt.Sprintf("\033[0;32m⚙ Running %s...\u001B[0m\n", program)))
	programPath, err := exec.LookPath(program)
	if err != nil {
		return err
	}
	env = append(env, fmt.Sprintf("PATH=%s", os.Getenv("PATH")))
	env = append(env, fmt.Sprintf("TMP=%s", os.Getenv("TMP")))
	env = append(env, fmt.Sprintf("TEMP=%s", os.Getenv("TEMP")))
	cmd := &exec.Cmd{
		Path:   programPath,
		Args:   append([]string{programPath}, args...),
		Env:    env,
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,
	}
	if err := cmd.Start(); err != nil {

		return err
	}
	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func writeOutput(
	version string,
	registry string,
	tag string,
	stdout *bytes.Buffer,
	err error,
) {
	output := ""
	prefix := "\033[0;32m✅ "
	if err != nil {
		prefix = "\033[0;31m❌ "
	}
	output += fmt.Sprintf(
		"::group::%sversion=%s registry=%s tag=%s\n",
		prefix,
		version,
		registry,
		tag,
	)
	output += stdout.String()
	if err != nil {
		output += fmt.Sprintf("\033[0;31m%s\033[0m\n", err.Error())
	}
	output += "::endgroup::\n"
	if _, err := os.Stdout.Write([]byte(output)); err != nil {
		panic(err)
	}
}

func buildVersion(
	version string,
	tags []string,
	date string,
	registries map[string]registry,
	push bool,
	githubToken string,
) error {
	var newTags []string
	for _, tag := range tags {
		newTags = append(newTags, tag)
		newTags = append(newTags, fmt.Sprintf("%s-%s", tag, date))
	}

	for registryName, registry := range registries {
		for _, tag := range newTags {
			stdout := &bytes.Buffer{}
			env := []string{
				fmt.Sprintf("GITHUB_TOKEN=%s", githubToken),
				fmt.Sprintf("REGISTRY=%s/", registryName),
			}

			if err := runExternalProgram(
				"docker",
				[]string{
					"build",
					".",
				},
				env,
				nil,
				stdout,
				stdout,
			); err != nil {
				err := fmt.Errorf(
					"build failed for version %s registry %s tag %s (%w)",
					version,
					registryName,
					tag,
					err,
				)
				writeOutput(version, registryName, tag, stdout, err)
				return err
			}

			if push {
				username := os.Getenv(registry.UserVariable)
				if username == "" {
					return fmt.Errorf(
						"cannot push: no username set in the %s environment variable",
						registry.UserVariable,
					)
				}
				password := os.Getenv(registry.PasswordVariable)
				if password == "" {
					return fmt.Errorf(
						"cannot push: no password set in the %s environment variable",
						registry.PasswordVariable,
					)
				}
				if err := runExternalProgram(
					"docker",
					[]string{
						"login",
						registryName,
						"-u",
						os.Getenv(registry.UserVariable),
						"--password-stdin",
					},
					env,
					bytes.NewBuffer([]byte(password)),
					stdout,
					stdout,
				); err != nil {
					err := fmt.Errorf(
						"push failed for version %s tag %s registry %s (%w)",
						version,
						tag,
						registryName,
						err,
					)
					writeOutput(version, registryName, tag, stdout, err)
					return err
				}
				if err := runExternalProgram(
					"docker-compose",
					[]string{
						"push",
					},
					env,
					nil,
					stdout,
					stdout,
				); err != nil {
					err := fmt.Errorf(
						"push failed for version %s tag %s registry %s (%w)",
						version,
						tag,
						registryName,
						err,
					)
					writeOutput(version, registryName, tag, stdout, err)
					return err
				}
			}
			writeOutput(version, registryName, tag, stdout, nil)
		}
	}

	return nil
}

type config struct {
	Revision   string              `yaml:"revision"`
	Versions   map[string][]string `yaml:"versions"`
	Registries map[string]registry `yaml:"registries"`
	Container  string              `default:"all"`
}

func listPackagesFromFile(source_project string) []string {
	var pwd, err3 = os.Getwd()
	if err3 != nil {
		fmt.Println(err3)
	}
	var source_project_dir string = filepath.Join(pwd, source_project)

	list := make([]string, 0, 10)

	source_project_file, err4 := os.Open(source_project_dir)
	if err4 != nil {
		fmt.Println(err4)
	}
	defer source_project_file.Close()
	lst, _ := source_project_file.Readdir(-1)
	for _, file := range lst {
		if file.IsDir() {
			list = append(list, file.Name())
		}
	}
	return list
}

func filterContainerSelection(selection string, list []string) []string {
	if selection != "all" {
		list2 := make([]string, 0, 10)
		for _, container := range list {
			if container == selection {
				list2 = append(list2, container)
			}
		}
		list = list2
	}
	return list
}

func getConfig(configYamlPath string) *config {
	fh, err := os.Open(configYamlPath)
	if err != nil {
		log.Fatal(err)
	}
	data, err := io.ReadAll(fh)
	if err != nil {
		log.Fatal(err)
	}
	conf := &config{}
	if err := yaml.Unmarshal(data, conf); err != nil {
		log.Fatal(err)
	}
	if err := defaults.Set(conf); err != nil {
		log.Fatal(err)
	}
	return conf
}

func main() {
	push := false
	if len(os.Args) == 2 && os.Args[1] == "--push" {
		push = true
	}
	githubToken := os.Getenv("GITHUB_TOKEN")

	conf := getConfig("build.yaml")
	source_project := "fixtures/arcaflow-plugins/python/"
	list := listPackagesFromFile(source_project)
	list = filterContainerSelection(conf.Container, list)

	for _, img := range list {
		img_ctx := filepath.Join(source_project, img)
		fmt.Printf("%v", img_ctx)
		os.Chdir(img_ctx)
		for version, tags := range conf.Versions {
			if err := buildVersion(version, tags, conf.Revision, conf.Registries, push, githubToken); err != nil {
				log.Fatal(err)
			}
		}
	}
}

package main

import (
	"crypto/md5"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

type AppMetadata struct {
	Name              string              `json:"name"`
	UniqueId          string              `json:"uniqueId"`
	ShortDescription  string              `json:"shortDescription"`
	Description       string              `json:"description"`
	Source            string              `json:"source"`
	Banner            string              `json:"banner"`
	SupportedReleases []string            `json:"supportedReleases"`
	Releases          map[string]*Release `json:"releases"`
}
type Release struct {
	Image          string `json:"image"`
	Command        string `json:"command"`
	SourceTarball  string `json:"sourceTarball"`
	WebXDCDownload string `json:"WebXDCDownload"`
	XdcSha512sum   string `json:"xdcsha512sum"`
	TarSha512sum   string `json:"tarsha512sum"`
}

type RepositoryMetadata struct {
	Apps map[string]*AppMetadata `json:"apps"`
}

var repo RepositoryMetadata

func init() {
	repo.Apps = map[string]*AppMetadata{}
}
func init() {
	// Define a custom log format that includes the short file name
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func sha512String(file string) string {
	f, err := os.Open(file)
	if err != nil {
		log.Fatalln(err)
	}
	defer f.Close()

	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Fatalln(err)
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

func main() {
	if _, err := os.Stat("apps"); err != nil {
		log.Println("'apps' directory is not available, are you in correct repository?")
		return
	}
	if _, err := os.Stat("build"); err != nil {
		log.Println("'build' directory is not available, are you in correct repository?")
		return
	}

	b, err := os.ReadFile("build/meta.json")
	if err == nil {
		err := json.Unmarshal(b, &repo)
		if err != nil {
			log.Fatalln(err)
		}
	}
	err = filepath.Walk("apps", walk)
	if err != nil {
		log.Println(err)
	}
}

var saveMutex sync.Mutex

func save() {
	saveMutex.Lock()

	b, err := json.MarshalIndent(repo, "", "    ")
	if err != nil {
		log.Fatalln(err)
	}
	err = os.WriteFile("build/meta.json", b, 0750)
	if err != nil {
		log.Fatalln(err)
	}
	saveMutex.Unlock()
}

func walk(p string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	if !strings.HasSuffix(info.Name(), ".json") {
		return nil
	}
	dirname := path.Dir(p)
	basename := path.Base(p)
	log.Println(dirname, basename, info.Size())
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var app AppMetadata
	err = json.Unmarshal(b, &app)
	if err != nil {
		return err
	}
	if _, ok := repo.Apps[app.UniqueId]; !ok {
		repo.Apps[app.UniqueId] = &AppMetadata{
			Name:              app.Name,
			UniqueId:          app.UniqueId,
			ShortDescription:  app.ShortDescription,
			Description:       app.Description,
			Source:            app.Source,
			Banner:            path.Join(app.UniqueId, app.Banner),
			SupportedReleases: app.SupportedReleases,
		}
	} else {
		repo.Apps[app.UniqueId].Name = app.Name
		repo.Apps[app.UniqueId].UniqueId = app.UniqueId
		repo.Apps[app.UniqueId].ShortDescription = app.ShortDescription
		repo.Apps[app.UniqueId].Description = app.Description
		repo.Apps[app.UniqueId].Source = app.Source
		repo.Apps[app.UniqueId].Banner = path.Join(app.UniqueId, app.Banner)
		repo.Apps[app.UniqueId].SupportedReleases = app.SupportedReleases
	}
	save()
	for i := range app.Releases {
		if _, ok := repo.Apps[app.UniqueId].Releases[i]; ok {
			log.Println("repo contains build of", app.UniqueId, "tag:", i)
			continue
		}
		if repo.Apps[app.UniqueId].Releases == nil {
			repo.Apps[app.UniqueId].Releases = make(map[string]*Release)
		}
		build(&app, i, app.Releases[i])
		repo.Apps[app.UniqueId].Releases[i] = &Release{
			Image:          app.Releases[i].Image,
			Command:        app.Releases[i].Command,
			SourceTarball:  path.Join(app.UniqueId, i, app.UniqueId+".tar.gz"),
			WebXDCDownload: path.Join(app.UniqueId, i, app.UniqueId+".xdc"),
			TarSha512sum:   sha512String(path.Join("build", app.UniqueId, i, app.UniqueId+".tar.gz")),
			XdcSha512sum:   sha512String(path.Join("build", app.UniqueId, i, app.UniqueId+".xdc")),
		}
		run("cp", path.Join(dirname, app.Banner), path.Join("build", app.UniqueId, app.Banner)) //, path.Join(outDir, app.UniqueId+".tar.gz"))
		save()
	}
	return nil
}

func build(app *AppMetadata, tag string, rel *Release) {
	log.Println("build:", app.UniqueId, "tag:", tag)
	outDir := path.Join("build", app.UniqueId, tag)

	tmpDir, err := os.MkdirTemp(os.TempDir(), "tmpbld")
	if err != nil {
		log.Fatalln(err)
	}
	run("docker", "run", "--rm", "-i", "-v", tmpDir+":/out", rel.Image, "/bin/bash", "-c", "git clone '"+app.Source+"' /build && cd /build && git checkout "+tag+" && tar --exclude-vcs -zcvf /out/source.tar.gz . && "+rel.Command)
	os.MkdirAll(outDir, 0750)
	run("mv", path.Join(tmpDir, "source.tar.gz"), path.Join(outDir, app.UniqueId+".tar.gz"))
	run("mv", path.Join(tmpDir, "app.xdc"), path.Join(outDir, app.UniqueId+".xdc"))
	if err != nil {
		log.Fatalln(err)
	}
}

func run(name string, args ...string) {
	log.Println("run:", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		log.Fatalln(err)
	}
}

func GetMD5Hash(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}

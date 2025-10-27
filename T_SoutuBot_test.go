package SoutuBot

import (
	"io"
	"os"
	"testing"

	fs "github.com/Miuzarte/FlareSolverr-go"
)

func TestSoutuBot(t *testing.T) {
	const imgPath = `/home/miuzarte/bot/Miuzarte/NothingBot_v4/BotEHentaiCache/3138775/1.webp`
	f, err := os.Open(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient(fs.NewClient("http://127.0.0.1:8191/v1"))
	resp, err := client.Search(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", *resp)
}

package banner

import (
	"fmt"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
	"github.com/kyokomi/emoji/v2"
)

func GetStartBanner() {
	in := `# Get Build`

	out, _ := glamour.Render(in, "dark")
	fmt.Print(out)
}

func GetBuildFailedBanner(serverName string) {
	in := "# Build Failed\n"
	in += "Remote build **failed** but the remote server is **still running** at **%s**, please use the following command to\n"
	in += "destory all server currently running in the project regardless if it's created by ham or try to track\n"
	in += "the progress with ```ham get recipe```.\n"
	in += "```\n"
	in += " $ ham clean \n"
	in += "```\n"
	in += "\n\n"

	in = fmt.Sprintf(in, serverName)

	out, _ := glamour.Render(in, "auto")
	fmt.Print(out)
}

func GetConnectFailBanner(serverName string) {
	in := "# SSH Connection Failed\n"
	in += "Cannot SSH into the remote server, it is *possible* that the remote server is **still running** at **%s**,"
	in += " please use the following command to destory all server currently running in the project regardless if it's"
	in += " created by ham or try again to track the progress with ```ham get recipe```.\n"
	in += "```\n"
	in += " $ ham clean \n"
	in += "```\n"
	in += "\n\n"

	in = fmt.Sprintf(in, serverName)

	out, _ := glamour.Render(in, "auto")
	fmt.Print(out)
}

func GetMalformedJSONBanner(serverName string) {
	in := "# Tracking Failed\n"
	in += "Cannot get builder status but the build is **still running** at **%s**, please use the following command to\n"
	in += "destory all server currently running in the project regardless if it's created by ham or try again to track\n"
	in += "the progress with ```ham get recipe```.\n"
	in += "```\n"
	in += " $ ham clean \n"
	in += "```\n"
	in += "\n\n"

	in = fmt.Sprintf(in, serverName)

	out, _ := glamour.Render(in, "auto")
	fmt.Print(out)
}

func GetRecipeBanner(name string, ver string, hash string) {
	in := "# Recipe Information\n"
	in += "**Name**: *%s* [%s]\n\n"
	in += "**SHA-256**: %s"
	in = fmt.Sprintf(in, name, ver, hash)

	out, _ := glamour.Render(in, "auto")
	fmt.Print(out)
}

func GetCmdProgressBanner() {
	in := "# Progress\n"
	out, _ := glamour.Render(in, "auto")
	fmt.Print(out)
}

func GetServerPriceInformationBanner(name string, price float64) {
	in := "# Price Information\n"
	in += "Server Name: %s\n\n"
	in += "Gross price: **%.4f** euros/hour.\n\n"
	in += "The bill is this rate times the hours the server exists, plus the 400 GB volume. "
	in += "The server and volume are deleted when the build finishes. "
	in += "A later `ham get` or `ham clean` also deletes build servers older than 24 hours. "
	in += "That sweep does not stop a build that is still running, so check the Hetzner project after `ham get` "
	in += "and run `ham clean` if a server is left behind.\n"
	in = fmt.Sprintf(in, name, price)

	out, _ := glamour.Render(in, "auto")
	fmt.Print(out)
}

func GetQuestionBanner() {
	in := "# Quesions\n"
	out, _ := glamour.Render(in, "auto")
	fmt.Print(out)
}

func GetRecipeNotExistsBanner() {
	c := color.New(color.FgYellow)
	c.Print(" Recipe does not exist locally, Using GIT.\n")
}

func GetFinishBanner() {
	c := color.New(color.FgGreen).Add(color.Bold)
	c.Print("Built Successfully ", emoji.Sprint(":rocket:"))
	fmt.Print("\n")
}

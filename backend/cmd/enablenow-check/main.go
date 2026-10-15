// Command enablenow-check runs the issuer pipeline for one existing consent
// against the real enablenow API and prints the computed credentials. It is a
// debugging aid: it never deletes the consent.
//
//	go run ./cmd/enablenow-check --config ../local-secrets/psd2-issuer/config.json --consent <id>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"psd2-issuer/credentials"
	"psd2-issuer/enablenow"
	"time"
)

type config struct {
	EnablenowConfig enablenow.Config `json:"enablenow_config"`
	Categorization  struct {
		Profile     string `json:"profile"`
		CountryCode string `json:"country_code"`
	} `json:"categorization"`
}

func main() {
	configPath := flag.String("config", "", "issuer config.json")
	consentId := flag.String("consent", "", "consent id")
	flag.Parse()
	if err := run(*configPath, *consentId); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(configPath, consentId string) error {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	if cfg.Categorization.Profile == "" {
		cfg.Categorization.Profile = "retail"
	}
	client, err := enablenow.NewHttpClient(cfg.EnablenowConfig)
	if err != nil {
		return err
	}
	ctx := context.Background()

	consent, err := client.GetConsent(ctx, consentId)
	if err != nil {
		return fmt.Errorf("get consent: %w", err)
	}
	fmt.Printf("consent: status=%s provider=%s has_transactions=%v\n", consent.Status, consent.ProviderKey, consent.HasTransactions())

	jobs, err := client.ListCategorizationJobs(ctx, consentId)
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	var job *enablenow.Job
	for _, j := range jobs {
		fmt.Printf("existing job: %s engine=%s profile=%s status=%s\n", j.Id, j.Engine, j.Profile, j.Status)
		if j.Engine == "e1" && j.Profile == cfg.Categorization.Profile && j.Status != enablenow.JobFailed {
			job = &j
		}
	}
	if job == nil {
		job, err = client.CreateCategorizationJob(ctx, enablenow.JobRequest{
			Engine: "e1", Profile: cfg.Categorization.Profile, CountryCode: cfg.Categorization.CountryCode, ConsentId: consentId,
		})
		if err != nil {
			return fmt.Errorf("create job: %w", err)
		}
		fmt.Printf("created job: %s status=%s\n", job.Id, job.Status)
	}
	for job.Status != enablenow.JobCompleted {
		if job.Status == enablenow.JobFailed {
			return fmt.Errorf("job %s failed", job.Id)
		}
		time.Sleep(2 * time.Second)
		if job, err = client.GetCategorizationJob(ctx, job.Id); err != nil {
			return fmt.Errorf("get job: %w", err)
		}
		fmt.Printf("job: status=%s\n", job.Status)
	}

	opts := credentials.Options{Months: 12, Rounding: 10, Now: time.Now()}
	from, to := credentials.Window(opts)
	txs, err := client.ListTransactions(ctx, consentId, from, to)
	if err != nil {
		return fmt.Errorf("list transactions: %w", err)
	}
	categorized := 0
	categories := map[string]int{}
	for _, tx := range txs {
		if tx.E1() != "" {
			categorized++
			categories[tx.E1()]++
		}
	}
	fmt.Printf("transactions in window %s..%s: %d (%d with an E1 category)\n", from.Format("2006-01-02"), to.Format("2006-01-02"), len(txs), categorized)
	for code, n := range categories {
		fmt.Printf("  %-45s %d\n", code, n)
	}

	accounts, err := client.ListAccounts(ctx, consentId)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	fmt.Printf("accounts: %d\n", len(accounts))

	for _, c := range credentials.Compute(accounts, txs, consent.ProviderKey, opts) {
		out, _ := json.Marshal(c.Attributes)
		fmt.Printf("%s: %s\n", c.Key, out)
	}
	return nil
}

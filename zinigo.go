// Downloader + optional page stripping using qpdf+awk
// Enable with -stripmenus. Requires qpdf and awk on PATH when -stripmenus is used.
// Strip/Merge debug is OFF by default; enable with -debug-stripmerge.
// After merge, we repack with qpdf to remove filename-based bookmarks so ToC shows plain page numbers.

package main

import (
	"bufio"
	"bytes"
	crand "crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SR-G/argweave"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

/* ────────────────────────────────────────────────────────────────────
   Service selection
   ──────────────────────────────────────────────────────────────────── */

type serviceCfg struct {
	baseURL     string
	newsstandID int
}

func pickService(unlimited bool) serviceCfg {
	if unlimited {
		return serviceCfg{"https://www.ziniounlimited.com", 3975}
	}
	return serviceCfg{"https://www.zinio.com", 101}
}

/* ────────────────────────────────────────────────────────────────────
   Globals / helpers
   ──────────────────────────────────────────────────────────────────── */

const (
	projectParam = "99"
	loginPath    = "/api/x7b9q-sync"

	defaultFP  = "abcd123"
	maxRetries = 3
	retrySleep = 2 * time.Second

	dirPerm   = 0o755
	padSpaces = "                                                                                                    "
)

var (
	debugHTTP bool
	client    *http.Client
)

func initHTTP(debug bool) {
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	client = &http.Client{Transport: tr}
	debugHTTP = debug
}
func dumpReq(resp *http.Response, body []byte) {
	if !debugHTTP {
		return
	}
	b, _ := httputil.DumpRequestOut(resp.Request, true)
	fmt.Println("----------- HTTP REQ -----------")
	fmt.Println(string(b))
	fmt.Println("----------- HTTP RESP ----------")
	fmt.Printf("%s %s\n", resp.Proto, resp.Status)
	for k, v := range resp.Header {
		fmt.Printf("%s: %s\n", k, strings.Join(v, ","))
	}
	fmt.Printf("\n%s\n", string(body))
	fmt.Println("--------------------------------")
}
func progressLine(s string) { fmt.Printf("\r%s%s\r", s, padSpaces) }

func safeName(s string) string {
	for _, r := range []string{"/", "\\", "<", ">", ":", "\"", "|", "?", "*"} {
		s = strings.ReplaceAll(s, r, "_")
	}
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return wd
}
func fileExists(fn string) bool { inf, err := os.Stat(fn); return err == nil && !inf.IsDir() }
func ensureDir(p string)        { _ = os.MkdirAll(p, dirPerm) }

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return err
	}
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = io.Copy(d, s)
	return err
}
func writeJSON(path string, v any) {
	_ = os.MkdirAll(filepath.Dir(path), dirPerm)
	b, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(path, b, 0o644)
}
func isNotEncrypted(err error) bool {
	if err == nil {
		return false
	}
	e := strings.ToLower(err.Error())
	return strings.Contains(e, "not encrypted") || strings.Contains(e, "unencrypted")
}

/* ────────────────────────────────────────────────────────────────────
   Main
   ──────────────────────────────────────────────────────────────────── */

//go:generate go run github.com/SR-G/argweave/cmd/weavedoc -type=parameters -file=zinigo.go -edit=README.md
//go:generate go run github.com/SR-G/argweave/cmd/weavehelp -type=parameters -file=zinigo.go -name=ZiniGo -edit=README.md -code-block=false
type parameters struct {
	Unlimited       bool   `arg:"long=unlimited,env=ZINIGO_UNLIMITED,help=Use Zinio Unlimited"`
	Email           string `arg:"short=u,long=email,env=ZINIGO_EMAIL,required,help=Zinio email"`
	Password        string `arg:"short=p,long=password,env=ZINIGO_PASSWORD,required,secret,help=Zinio password"`
	Fingerprint     string `arg:"long=fingerprint,env=ZINIGO_FINGERPRINT,default=abcd123,help=Device fingerprint"`
	KeepTemps       bool   `arg:"long=keep-temp,env=ZINIGO_KEEP_TEMP,help=Keep per-page PDFs instead of deleting them"`
	Verbose         bool   `arg:"short=v,long=verbose,env=ZINIGO_VERBOSE,help=Enable verbose logs"`
	DebugHTTP       bool   `arg:"long=debug-http,env=ZINIGO_DEBUG_HTTP,help=Dump HTTP traffic"`
	AnalyzeOnly     bool   `arg:"long=analyze,env=ZINIGO_ANALYZE,help=Only analyze"`
	IssueID         int    `arg:"long=issue,env=ZINIGO_ISSUE,help=Specific issue ID"`
	OutputDir       string `arg:"long=output,env=ZINIGO_OUTPUT,default=issues,help=Output directory"`
	DownloadThumbs  bool   `arg:"long=thumbs,env=ZINIGO_THUMBS,help=Download page thumbnails"`
	StripMenus      bool   `arg:"long=stripmenus,env=ZINIGO_STRIPMENUS,help=Strip small TL badge via qpdf and awk"`
	DebugStripMerge bool   `arg:"long=debug-stripmerge,env=ZINIGO_DEBUG_STRIPMERGE,help=Enable debug logs around strip and merge"`
	DebugDir        string `arg:"long=debug-dir,env=ZINIGO_DEBUG_DIR,default=debug,help=Directory for per-page debug dumps"`
}

func normalizeLegacyFlags(args []string) ([]string, error) {
	normalized := make([]string, len(args))
	afterSeparator := false
	for i, arg := range args {
		if arg == "--" {
			afterSeparator = true
		} else if !afterSeparator && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			name := strings.TrimPrefix(arg, "-")
			name, _, _ = strings.Cut(name, "=")
			if len(name) > 1 {
				arg = "-" + arg
			}
		}
		normalized[i] = arg
	}
	return normalized, nil
}

func main() {
	var params parameters
	parser, err := argweave.New(&params, argweave.AppConfig{
		Name:     "ZiniGo",
		PreParse: normalizeLegacyFlags,
	})
	if err != nil {
		log.Fatalf("configure command line: %v", err)
	}
	parser.ParseAndHandleExitIfNeeded(os.Args[1:])

	cfg := pickService(params.Unlimited)

	if params.Verbose || params.AnalyzeOnly {
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	} else {
		log.SetOutput(io.Discard)
	}
	initHTTP(params.DebugHTTP)

	fmt.Println("🔐 Logging in...")
	login, err := loginSync(params.Email, params.Password, params.Fingerprint, cfg)
	if err != nil {
		fmt.Printf("❌ login failed: %v\n", err)
		return
	}

	analysisDir := filepath.Join(mustGetwd(), "analysis")
	ensureDir(analysisDir)

	if params.IssueID > 0 && params.AnalyzeOnly {
		analyzeSpecificIssue(login, params.IssueID, cfg, analysisDir)
		return
	}

	fmt.Println("📚 Fetching library...")
	issues, err := fetchLibrary(login, cfg, params.Unlimited) // <-- filter only when -unlimited is set
	if err != nil {
		fmt.Printf("❌ library fetch failed: %v\n", err)
		return
	}
	fmt.Printf("✓ Found %d issues\n\n", len(issues))

	outRoot := filepath.Join(mustGetwd(), params.OutputDir)
	ensureDir(outRoot)

	thumbsRoot := ""
	if params.DownloadThumbs {
		thumbsRoot = filepath.Join(mustGetwd(), "thumbnails")
		ensureDir(thumbsRoot)
	}

	// If stripping is enabled, make sure qpdf and awk are available.
	if params.StripMenus {
		if err := ensureCmdOnPath("qpdf"); err != nil {
			fmt.Printf("❌ -stripmenus requested but qpdf not found: %v\n", err)
			return
		}
		if err := ensureCmdOnPath("awk"); err != nil {
			fmt.Printf("❌ -stripmenus requested but awk not found: %v\n", err)
			return
		}
	}

	success, fail := 0, 0
	var results []resultLine

	for idx, it := range issues {
		title := safeName(it.Publication.Name) + " - " + safeName(it.Name)
		outPDF := filepath.Join(outRoot, title+".pdf")
		prefix := fmt.Sprintf("[%d/%d] %s", idx+1, len(issues), title)

		if fileExists(outPDF) && !params.AnalyzeOnly {
			progressLine(prefix + " OK (exists)")
			fmt.Println()
			success++
			results = append(results, resultLine{Title: title, Pages: it.NoOfPages, OK: true})
			if params.DownloadThumbs {
				ensureThumbnails(login, it.ID, title, thumbsRoot, prefix, cfg, params.Verbose)
			}
			continue
		}

		if params.Verbose {
			fmt.Println()
			log.Printf("processing issue %d (%s)", it.ID, title)
		}

		det, err := getIssueDetailsWithRetry(login, it.ID, cfg)
		if err != nil {
			fail++
			results = append(results, resultLine{Title: title, Pages: it.NoOfPages, Err: err})
			progressLine(prefix + " ERROR (details)")
			fmt.Println()
			continue
		}

		passwords := gatherPasswords(det, it.ID, it.Publication.Name, params.Verbose)
		pages := det.Data.Pages
		if len(pages) == 0 {
			tmp, err := getPages(login, it.ID, cfg)
			if err != nil {
				fail++
				results = append(results, resultLine{Title: title, Pages: it.NoOfPages, Err: err})
				progressLine(prefix + " ERROR (pages)")
				fmt.Println()
				continue
			}
			pages = tmp.Data.Pages
		}

		if params.DownloadThumbs {
			ensureThumbnailsFromPages(pages, title, thumbsRoot, prefix, params.Verbose)
		}
		if params.AnalyzeOnly {
			progressLine(prefix + " (analyze-only)")
			fmt.Println()
			continue
		}

		progressLine(fmt.Sprintf("%s [%d pages]", prefix, len(pages)))

		// Prepare debug scaffolding
		issueDebugDir := filepath.Join(params.DebugDir, safeName(title))
		if params.DebugStripMerge {
			ensureDir(issueDebugDir)
		}
		dbg := newIssueDebug(title, outPDF, issueDebugDir)

		tempFiles := make([]string, 0, len(pages))
		var procErr error

		for i, pg := range pages {
			if pg.Src == "" {
				continue
			}
			pageIndex := i + 1
			tmpPath := filepath.Join(outRoot, fmt.Sprintf("%d_%s_%s.temp.pdf", it.ID, pg.Index, randSuffix()))

			// Download
			var size int64
			for attempt := 1; attempt <= maxRetries; attempt++ {
				progressLine(fmt.Sprintf("%s [page %d/%d] downloading…", prefix, pageIndex, len(pages)))
				size, procErr = downloadToFile(pg.Src, tmpPath)
				if procErr == nil && size > 0 {
					break
				}
				time.Sleep(retrySleep)
			}
			dbg.PageLog(pageIndex, "download", map[string]any{
				"url":  pg.Src,
				"size": size,
				"err":  errString(procErr),
			})
			if procErr != nil || size == 0 {
				procErr = fmt.Errorf("page %d download failed", pageIndex)
				break
			}

			// Decrypt
			progressLine(fmt.Sprintf("%s [page %d/%d] decrypting…", prefix, pageIndex, len(pages)))
			decNote, derr := decryptWithStrategies(tmpPath, passwords, det, analysisDir, params.Verbose)
			dbg.PageLog(pageIndex, "decrypt", map[string]any{
				"note": decNote, "err": errString(derr),
			})
			if derr != nil {
				procErr = fmt.Errorf("page %d decrypt: %w", pageIndex, derr)
				break
			}

			// Save pre-strip copy (debug)
			if params.DebugStripMerge {
				_ = copyFile(tmpPath, filepath.Join(issueDebugDir, fmt.Sprintf("page_%03d_pre.pdf", pageIndex)))
				preVal := validateFileBrief(tmpPath)
				dbg.PageLog(pageIndex, "validate_pre", preVal)
			}

			// Remove annotations first (best-effort)
			_ = api.RemoveAnnotationsFile(tmpPath, "", []string{"1"}, nil, nil, nil, true)

			// Shell-equivalent stripping
			if params.StripMenus {
				progressLine(fmt.Sprintf("%s [page %d/%d] stripmenus…", prefix, pageIndex, len(pages)))
				sinfo, sErr := stripMenusShell(tmpPath, params.DebugStripMerge)
				dbg.PageLog(pageIndex, "stripmenus", sinfo)
				if sErr != nil && params.DebugStripMerge {
					fmt.Printf("\n[stripmenus] page %d: %v\n\n", pageIndex, sErr)
				}
			}

			// Save post-strip copy (debug) + validate
			if params.DebugStripMerge {
				_ = copyFile(tmpPath, filepath.Join(issueDebugDir, fmt.Sprintf("page_%03d_post.pdf", pageIndex)))
				postVal := validateFileBrief(tmpPath)
				dbg.PageLog(pageIndex, "validate_post", postVal)
			}

			tempFiles = append(tempFiles, tmpPath)
		}

		if procErr != nil {
			for _, f := range tempFiles {
				_ = os.Remove(f)
			}
			fail++
			results = append(results, resultLine{Title: title, Pages: it.NoOfPages, Err: procErr, Downloaded: len(tempFiles)})
			progressLine(fmt.Sprintf("%s ERROR (%d/%d)", prefix, len(tempFiles), len(pages)))
			fmt.Println()
			dbg.Error = procErr.Error()
			dbg.DoneWrite()
			continue
		}

		// MERGE with diagnostics
		progressLine(fmt.Sprintf("%s merging…", prefix))

		if params.DebugStripMerge {
			pageSumm := make([]any, 0, len(tempFiles))
			for pi, f := range tempFiles {
				pageSumm = append(pageSumm, map[string]any{
					"index": pi + 1,
					"file":  filepath.Base(f),
					"valid": validateFileBrief(f),
				})
			}
			dbg.Merge = map[string]any{
				"page_count": len(tempFiles),
				"pages":      pageSumm,
			}
		}

		mergeErr := api.MergeCreateFile(tempFiles, outPDF, false, nil)
		if mergeErr != nil {
			if params.DebugStripMerge {
				fmt.Printf("\n[merge] first attempt failed: %v\n", mergeErr)
			}
			conf := model.NewDefaultConfiguration()
			conf.ValidationMode = model.ValidationRelaxed
			mergeErr2 := api.MergeCreateFile(tempFiles, outPDF, false, conf)
			if mergeErr2 != nil {
				if params.DebugStripMerge {
					fmt.Printf("[merge] relaxed attempt failed: %v\n", mergeErr2)
				}
				if !params.KeepTemps {
					for _, f := range tempFiles {
						_ = os.Remove(f)
					}
				}
				_ = os.Remove(outPDF)
				fail++
				results = append(results, resultLine{Title: title, Pages: it.NoOfPages, Err: mergeErr2})
				progressLine(prefix + " ERROR (merge)")
				fmt.Println()

				dbg.MergeError = mergeErr.Error()
				dbg.MergeError2 = mergeErr2.Error()
				dbg.DoneWrite()
				continue
			}
			dbg.MergeNote = "succeeded with relaxed validation"
		} else {
			dbg.MergeNote = "succeeded (default validation)"
		}

		// Remove annotations in final (best-effort)
		_ = api.RemoveAnnotationsFile(outPDF, "", []string{"1-"}, nil, nil, nil, true)

		// Normalize the document outline (ToC)
		_ = normalizeTOC(outPDF, len(pages), params.DebugStripMerge)

		// Delete temp page PDFs unless asked to keep
		if !params.KeepTemps {
			for _, f := range tempFiles {
				_ = os.Remove(f)
			}
		} else if params.DebugStripMerge {
			fmt.Printf("[merge] keeping %d temp page PDFs due to -keep-temp\n", len(tempFiles))
		}

		progressLine(fmt.Sprintf("%s OK (%d pages)", prefix, len(pages)))
		fmt.Println()

		success++
		results = append(results, resultLine{Title: title, Pages: it.NoOfPages, OK: true})

		dbg.FinalValidate = validateFileBrief(outPDF)
		dbg.DoneWrite()

		if params.DownloadThumbs {
			ensureThumbnails(login, it.ID, title, thumbsRoot, prefix, cfg, params.Verbose)
		}
	}

	// Summary
	fmt.Printf("\n%s\n", strings.Repeat("=", 50))
	fmt.Printf("Summary: %d OK  %d ERR\n", success, fail)
	for _, r := range results {
		if !r.OK {
			fmt.Printf("  • %s (%d/%d)\n", r.Title, r.Downloaded, r.Pages)
		}
	}
	if fail > 0 {
		os.Exit(1)
	}
}

/* ────────────────────────────────────────────────────────────────────
   Networking
   ──────────────────────────────────────────────────────────────────── */

type loginResp struct {
	Status bool `json:"status"`
	Data   struct {
		User struct {
			UserIDString string `json:"user_id_string"`
		} `json:"user"`
	} `json:"data"`
	cookies []*http.Cookie
	token   string
}

func loginSync(email, password, fp string, svc serviceCfg) (*loginResp, error) {
	payload := map[string]any{
		"email": email, "password": password,
		"device": map[string]any{
			"name": "Windows Chrome", "fingerprint": fp, "device_type": "Desktop", "client_type": "Web",
		},
		"newsstand": map[string]any{
			"id": svc.newsstandID, "currency": "USD", "country": "US",
			"name": "Zinio", "cc": "us", "localeCode": "en_US",
			"userLang": "en_US", "userCountry": "US", "userCurrency": "USD",
			"requiresCookies": true, "requiresExplicitConsent": true, "requiresAdultConfirmation": true,
			"adWords": map[string]any{"id": 1, "label": ""}, "isDefaultNewsstand": true,
		},
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", svc.baseURL+loginPath, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	dumpReq(resp, body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login %s: %s", resp.Status, body)
	}
	var lr loginResp
	if json.Unmarshal(body, &lr) != nil {
		return nil, errors.New("unmarshal login")
	}
	lr.cookies = resp.Cookies()
	for _, c := range lr.cookies {
		if c.Name == "zwrt" {
			lr.token = c.Value
		}
	}
	if lr.token == "" {
		return nil, errors.New("zwrt token not found")
	}
	return &lr, nil
}

type libraryResp struct{ Data []libraryItem }

type libraryItem struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	NoOfPages   int    `json:"no_of_pages"`
	Publication struct {
		Name string `json:"name"`
	} `json:"publication"`
	Checkout *struct {
		IsCheckout bool `json:"is_checkout"`
		Archived   bool `json:"archived"`
	} `json:"checkout"`
}

func fetchLibrary(lr *loginResp, svc serviceCfg, onlyCheckedOut bool) ([]libraryItem, error) {
	var all []libraryItem
	offset, limit := 0, 100
	for {
		//                   /api/newsstand/newsstands/108/users/2636044247760896354/library-issues?limit=100&offset=0&from_date=2026-11-24+17%3A36%3A39&status=0%2C1%2C2&sort=-added_at
		query := url.Values{
			"limit":  {fmt.Sprint(limit)},
			"offset": {fmt.Sprint(offset)},
			"sort":   {"-added_at"},
		}
		endpoint := fmt.Sprintf("%s/api/newsstand/newsstands/%d/users/%s/library-issues?%s",
			svc.baseURL, svc.newsstandID, lr.Data.User.UserIDString, query.Encode())
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("library request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if svc.newsstandID != 101 {
			req.Header.Set("Authorization", "Bearer "+lr.token)
		}
		for _, c := range lr.cookies {
			req.AddCookie(c)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read library response: %w", err)
		}
		dumpReq(resp, body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("library %s: %s", resp.Status, body)
		}
		var lrsp libraryResp
		if json.Unmarshal(body, &lrsp) != nil {
			return nil, errors.New("unmarshal library")
		}

		// Only filter to checked-out items when using -unlimited
		for _, it := range lrsp.Data {
			if onlyCheckedOut {
				if it.Checkout != nil && it.Checkout.IsCheckout {
					all = append(all, it)
				}
			} else {
				all = append(all, it)
			}
		}

		if len(lrsp.Data) == 0 {
			break
		}
		offset += limit
	}
	return all, nil
}

/* ────────────────────────────────────────────────────────────────────
   Reader/content
   ──────────────────────────────────────────────────────────────────── */

type pageInfo struct{ Index, Src, Thumbnail string }
type issueDetails struct {
	Data struct {
		Issue struct {
			ID         int
			Hash       string `json:"hash"`
			LegacyHash string `json:"legacy_hash"`
		} `json:"issue"`
		Pages []pageInfo `json:"pages"`
	} `json:"data"`
}

func readerGET(lr *loginResp, svc serviceCfg, url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+lr.token)
	for _, c := range lr.cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	dumpReq(resp, body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %s: %s", resp.Status, body)
	}
	return body, nil
}

func getIssueDetails(lr *loginResp, id int, svc serviceCfg) (*issueDetails, error) {
	u := fmt.Sprintf("%s/api/reader/content?issue_id=%d&newsstand_id=%d&user_id=%s&format=pdf&project=%s&logger=null",
		svc.baseURL, id, svc.newsstandID, lr.Data.User.UserIDString, projectParam)
	b, err := readerGET(lr, svc, u)
	if err != nil {
		return nil, err
	}
	var det issueDetails
	return &det, json.Unmarshal(b, &det)
}
func getPages(lr *loginResp, id int, svc serviceCfg) (*issueDetails, error) {
	u := fmt.Sprintf("%s/api/reader/content?issue_id=%d&newsstand_id=%d&user_id=%s", svc.baseURL, id, svc.newsstandID, lr.Data.User.UserIDString)
	b, err := readerGET(lr, svc, u)
	if err != nil {
		return nil, err
	}
	var det issueDetails
	return &det, json.Unmarshal(b, &det)
}
func getIssueDetailsWithRetry(lr *loginResp, id int, svc serviceCfg) (*issueDetails, error) {
	det, err := getIssueDetails(lr, id, svc)
	if err == nil && det.Data.Issue.Hash != "" {
		return det, nil
	}
	return det, err
}

/* ────────────────────────────────────────────────────────────────────
   Thumbnails
   ──────────────────────────────────────────────────────────────────── */

func ensureThumbnails(lr *loginResp, id int, title, root, prefix string, svc serviceCfg, verbose bool) {
	det, err := getPages(lr, id, svc)
	if err == nil {
		ensureThumbnailsFromPages(det.Data.Pages, title, root, prefix, verbose)
	}
}
func ensureThumbnailsFromPages(p []pageInfo, title, root, prefix string, verbose bool) {
	if root == "" {
		return
	}
	dir := filepath.Join(root, title+"-thumbnails")
	ensureDir(dir)

	total := 0
	for _, pg := range p {
		if pg.Thumbnail == "" {
			continue
		}
		f := filepath.Join(dir, pg.Index+".jpg")
		if !fileExists(f) {
			total++
		}
	}
	if total == 0 {
		progressLine(fmt.Sprintf("%s  ▣  up-to-date", prefix))
		fmt.Println()
		return
	}

	done := 0
	for _, pg := range p {
		if pg.Thumbnail == "" {
			continue
		}
		f := filepath.Join(dir, pg.Index+".jpg")
		if fileExists(f) {
			continue
		}
		done++
		progressLine(fmt.Sprintf("%s  ▣  %d/%d", prefix, done, total))
		if _, err := downloadToFile(pg.Thumbnail, f); err != nil && verbose {
			log.Printf("thumbnail %s: %v", pg.Index, err)
		}
	}
	progressLine(fmt.Sprintf("%s  ▣  done (%d)", prefix, total))
	fmt.Println()
}

/* ────────────────────────────────────────────────────────────────────
   Password / decryption
   ──────────────────────────────────────────────────────────────────── */

func gatherPasswords(det *issueDetails, _ int, _ string, verbose bool) []string {
	add := func(dst *[]string, pw string) {
		if pw == "" {
			return
		}
		for _, v := range *dst {
			if v == pw {
				return
			}
		}
		*dst = append(*dst, pw)
	}

	pwds := []string{}
	if det.Data.Issue.Hash != "" {
		add(&pwds, det.Data.Issue.Hash)
		if len(det.Data.Issue.Hash) >= 32 {
			add(&pwds, det.Data.Issue.Hash[:32])
		}
	}
	if det.Data.Issue.LegacyHash != "" {
		add(&pwds, det.Data.Issue.LegacyHash)
		if len(det.Data.Issue.LegacyHash) >= 32 {
			add(&pwds, det.Data.Issue.LegacyHash[:32])
		}
	}
	base := append([]string(nil), pwds...)
	for _, pw := range base {
		if len(pw) > 32 {
			add(&pwds, pw[:32])
		}
		if len(pw) > 16 && len(pw) != 32 {
			add(&pwds, pw[:16])
		}
	}
	if verbose {
		log.Printf("%d password candidates", len(pwds))
	}
	return pwds
}

func decryptWithStrategies(path string, pwds []string, _ *issueDetails, _ string, verbose bool) (string, error) {
	type strat struct {
		name string
		key  int
		aes  bool
	}
	strats := []strat{{"AES-256", 256, true}, {"AES-128", 128, true}, {"RC4-128", 128, false}, {"RC4-40", 40, false}}

	for idx, pw := range pwds {
		if verbose {
			log.Printf("password %d/%d: %s", idx+1, len(pwds), maskPassword(pw))
		}
		for _, st := range strats {
			var c *model.Configuration
			if st.aes {
				c = model.NewAESConfiguration(pw, pw, st.key)
			} else {
				c = model.NewRC4Configuration(pw, pw, st.key)
			}
			c.ValidationMode = model.ValidationRelaxed
			if err := api.DecryptFile(path, "", c); err == nil {
				return "decrypted (" + st.name + ")", nil
			} else if isNotEncrypted(err) {
				return "not encrypted", nil
			}
		}
		for _, st := range strats {
			var c *model.Configuration
			if st.aes {
				c = model.NewAESConfiguration(pw, "", st.key)
			} else {
				c = model.NewRC4Configuration(pw, "", st.key)
			}
			c.ValidationMode = model.ValidationRelaxed
			if err := api.DecryptFile(path, "", c); err == nil {
				return "decrypted (" + st.name + ", user-pw only)", nil
			}
		}
	}
	for _, lpw := range []string{"", "password", "zinio"} {
		conf := model.NewRC4Configuration(lpw, lpw, 40)
		conf.ValidationMode = model.ValidationRelaxed
		if err := api.DecryptFile(path, "", conf); err == nil {
			return "decrypted (legacy default)", nil
		}
	}
	if err := api.ValidateFile(path, model.NewDefaultConfiguration()); err == nil {
		return "not encrypted", nil
	}
	return "", errors.New("all strategies failed")
}

/* ────────────────────────────────────────────────────────────────────
   Download helper
   ──────────────────────────────────────────────────────────────────── */

func downloadToFile(url, dest string) (int64, error) {
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("status %s: %s", resp.Status, b)
	}
	f, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return n, err
	}
	if n == 0 {
		return 0, errors.New("empty file")
	}
	return n, nil
}

/* ────────────────────────────────────────────────────────────────────
   Analyze-only helpers
   ──────────────────────────────────────────────────────────────────── */

func analyzeSpecificIssue(lr *loginResp, id int, svc serviceCfg, dir string) {
	det, err := getIssueDetails(lr, id, svc)
	if err != nil {
		log.Fatalf("detail: %v", err)
	}
	first := ""
	for _, pg := range det.Data.Pages {
		if pg.Src != "" {
			first = pg.Src
			break
		}
	}
	if first == "" {
		log.Fatalf("no page src")
	}
	out := filepath.Join(dir, fmt.Sprintf("test_%d_page1.pdf", id))
	if _, err := downloadToFile(first, out); err != nil {
		log.Fatalf("download: %v", err)
	}
	for _, pw := range gatherPasswords(det, id, "", true) {
		tryDirectDecryption(out, pw)
	}
}
func tryDirectDecryption(path, pw string) {
	cs := []struct {
		name string
		c    *model.Configuration
	}{
		{"AES-256", model.NewAESConfiguration(pw, pw, 256)},
		{"AES-128", model.NewAESConfiguration(pw, pw, 128)},
		{"RC4-128", model.NewRC4Configuration(pw, pw, 128)},
		{"RC4-40", model.NewRC4Configuration(pw, pw, 40)},
		{"AES-256 (user only)", model.NewAESConfiguration(pw, "", 256)},
		{"AES-128 (user only)", model.NewAESConfiguration(pw, "", 128)},
		{"RC4-128 (user only)", model.NewRC4Configuration(pw, "", 128)},
		{"RC4-40 (user only)", model.NewRC4Configuration(pw, "", 40)},
	}
	for _, v := range cs {
		v.c.ValidationMode = model.ValidationRelaxed
		if api.DecryptFile(path, "", v.c) == nil {
			log.Printf(" ✓ %s works", v.name)
			return
		}
	}
	log.Printf(" × all failed for %s", maskPassword(pw))
}

/* ────────────────────────────────────────────────────────────────────
   Results & debug structures
   ──────────────────────────────────────────────────────────────────── */

type resultLine struct {
	Title      string
	Pages      int
	OK         bool
	Err        error
	Downloaded int
}

type issueDebug struct {
	Title         string           `json:"title"`
	Output        string           `json:"output"`
	IssueDir      string           `json:"issue_dir"`
	Pages         []map[string]any `json:"pages"`
	Merge         map[string]any   `json:"merge,omitempty"`
	MergeNote     string           `json:"merge_note,omitempty"`
	MergeError    string           `json:"merge_error,omitempty"`
	MergeError2   string           `json:"merge_error2,omitempty"`
	FinalValidate map[string]any   `json:"final_validate,omitempty"`
	Error         string           `json:"error,omitempty"`
	outPath       string
}

func newIssueDebug(title, outPDF, dir string) *issueDebug {
	return &issueDebug{
		Title:    title,
		Output:   outPDF,
		IssueDir: dir,
		Pages:    make([]map[string]any, 0, 256),
		outPath:  filepath.Join(dir, "manifest.json"),
	}
}
func (d *issueDebug) PageLog(idx int, step string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["_page"] = idx
	data["_step"] = step
	d.Pages = append(d.Pages, data)
}
func (d *issueDebug) DoneWrite() { writeJSON(d.outPath, d) }

func validateFileBrief(path string) map[string]any {
	info := map[string]any{"file": filepath.Base(path)}
	fi, err := os.Stat(path)
	if err == nil {
		info["size"] = fi.Size()
	}
	conf := model.NewDefaultConfiguration()
	if err := api.ValidateFile(path, conf); err != nil {
		info["valid"] = false
		info["validate_err"] = err.Error()
	} else {
		info["valid"] = true
	}
	return info
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

/* ────────────────────────────────────────────────────────────────────
   Shell-equivalent page stripper (qpdf + awk)
   ──────────────────────────────────────────────────────────────────── */

func ensureCmdOnPath(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found on PATH", name)
	}
	return nil
}

// stripMenusShell replicates strip_hamburger.sh on a single-page PDF.
// NOTE: Accepts qpdf exit code 0 or 3 ("succeeded with warnings") as success.
func stripMenusShell(inpdf string, debug bool) (map[string]any, error) {
	workdir, err := os.MkdirTemp("", "stripmenus-*")
	if err != nil {
		return nil, err
	}
	// Clean up unless debug requested
	if !debug {
		defer os.RemoveAll(workdir)
	} else {
		fmt.Printf("[stripmenus] workdir: %s (qpdf+awk)…\n\n", workdir)
	}

	qdf := filepath.Join(workdir, "p1.qdf.pdf")
	workqdf := filepath.Join(workdir, "p1.work.pdf")
	dump := filepath.Join(workdir, "stream.txt")
	clean := filepath.Join(workdir, "stream.cleaned.txt")
	awkfile := filepath.Join(workdir, "strip_hamburger.awk")
	fixqdf := filepath.Join(workdir, "p1.fixed.qdf.pdf")
	outpdf := filepath.Join(workdir, "p1.cleaned.pdf")

	// 1) Convert to QDF (treat exit 0 or 3 as success)
	if err := runQpdfCmd("qpdf", "--qdf", "--object-streams=disable", inpdf, qdf); err != nil {
		return nil, fmt.Errorf("qpdf --qdf: %w", err)
	}
	if err := copyFile(qdf, workqdf); err != nil {
		return nil, err
	}

	// 2) Find page-1 content object id
	objid, err := firstPageContentObject(workqdf)
	if err != nil || objid == "" {
		return map[string]any{"note": "no obj id"}, fmt.Errorf("could not detect page 1 content object id: %v", err)
	}
	if debug {
		fmt.Printf("[stripmenus] page-1 /Contents object id: %s\n", objid)
	}

	// 3) Dump filtered stream data for that object
	if err := runQpdfCmdToFile(dump, "qpdf", "--show-object="+objid, "--filtered-stream-data", workqdf); err != nil {
		return nil, fmt.Errorf("qpdf --show-object: %w", err)
	}

	// 4) AWK program: identical logic to your strip_hamburger.sh
	awkProgram := `BEGIN {
  lvl = 0; killDepth = 0; openKill = 0
}
function rect_match(s,   m,x,y,w,h){
  if (match(s,/[ \t]*([-0-9.e+]+)[ \t]+([-0-9.e+]+)[ \t]+([-0-9.e+]+)[ \t]+([-0-9.e+]+)[ \t]+re[ \t]+W[ \t]+n([^0-9A-Za-z]|$)/,m)) {
    x=m[1]+0; y=m[2]+0; w=m[3]+0; h=m[4]+0
    if (x<=200 && y>=7300 && w<=600 && h<=600) return 1
  }
  return 0
}
{
  line=$0
  if (killDepth==0 && openKill==0 && rect_match(line)) {
    if (lvl>0) { killDepth = lvl } else { openKill = 1 }
  }
  nq = gsub(/(^|[ \t])q([ \t]|$)/, "&", line)
  nQ = gsub(/(^|[ \t])Q([ \t]|$)/, "&", line)
  if (killDepth==0 && openKill==0) print $0
  lvl += (nq - nQ)
  if (killDepth>0 && lvl < killDepth) killDepth=0
  if (openKill>0 && lvl==0 && nQ>0) openKill=0
}`
	if err := os.WriteFile(awkfile, []byte(awkProgram), 0o644); err != nil {
		return nil, err
	}
	if err := runCmdToFile(clean, "awk", "-f", awkfile, dump); err != nil {
		return nil, fmt.Errorf("awk filter: %w", err)
	}

	// If no change, skip (helps diagnose)
	orig, _ := os.ReadFile(dump)
	newb, _ := os.ReadFile(clean)
	changed := !bytes.Equal(orig, newb)
	if debug {
		if changed {
			fmt.Printf("[stripmenus] stream %s: rect match → stream changed (%d → %d bytes)\n", objid, len(orig), len(newb))
		} else {
			fmt.Printf("[stripmenus] stream %s: no change (no rect hit)\n", objid)
		}
	}

	if changed {
		// 5) Replace the stream for that object in the QDF
		if err := replaceStreamInQDF(workqdf, objid, clean); err != nil {
			return nil, fmt.Errorf("replace stream: %w", err)
		}
	}

	// 6) Normalize and rebuild back to a normal PDF, then overwrite original (accept qpdf 3)
	_ = runQpdfCmd("qpdf", "--qdf", "--object-streams=disable", workqdf, fixqdf) // best-effort
	if err := runQpdfCmd("qpdf", "--object-streams=generate", fixqdf, outpdf); err != nil {
		return nil, fmt.Errorf("qpdf regenerate: %w", err)
	}
	// move outpdf back to inpdf
	if err := copyFile(outpdf, inpdf); err != nil {
		return nil, fmt.Errorf("overwrite: %w", err)
	}

	return map[string]any{
		"object_id": objid,
		"changed":   changed,
		"note":      "cleaned (qpdf exit 0/3 accepted)",
	}, nil
}

// firstPageContentObject reads: qpdf --show-pages <qdf>, returns first content object id for page 1.
func firstPageContentObject(qdf string) (string, error) {
	cmd := exec.Command("qpdf", "--show-pages", qdf)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	inPage1 := false
	waitContent := false
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "page ") && strings.Contains(trim, "page 1:") {
			inPage1 = true
			waitContent = false
			continue
		}
		if !inPage1 {
			continue
		}
		if strings.HasPrefix(trim, "content:") {
			waitContent = true
			continue
		}
		if waitContent {
			if trim == "" {
				continue
			}
			parts := strings.Fields(trim)
			if len(parts) > 0 {
				re := regexp.MustCompile(`^(\d+)`)
				m := re.FindStringSubmatch(parts[0])
				if len(m) >= 2 {
					return m[1], nil
				}
			}
			break
		}
		if strings.HasPrefix(trim, "page ") && !strings.Contains(trim, "page 1:") {
			break
		}
	}
	return "", errors.New("no content id found for page 1")
}

// replaceStreamInQDF reads qdfIn, finds "<obj> 0 obj" then its "stream" block,
// and replaces the bytes up to "endstream" with the contents of cleanPath.
func replaceStreamInQDF(qdfIn string, objid string, cleanPath string) error {
	b, err := os.ReadFile(qdfIn)
	if err != nil {
		return err
	}
	newData, err := os.ReadFile(cleanPath)
	if err != nil {
		return err
	}
	// Find object header at line boundary
	reObj := regexp.MustCompile(`(?s)(^|\r?\n)` + regexp.QuoteMeta(objid) + `\s+0\s+obj\b`)
	loc := reObj.FindIndex(b)
	if loc == nil {
		return fmt.Errorf("object %s 0 obj not found", objid)
	}
	startAfterHeader := loc[1]

	// Find "stream" line (with EOL), then "endstream"
	reStream := regexp.MustCompile(`(?s)\r?\nstream\r?\n`)
	sLoc := reStream.FindIndex(b[startAfterHeader:])
	if sLoc == nil {
		return fmt.Errorf("stream start not found in obj %s", objid)
	}
	streamDataStart := startAfterHeader + sLoc[1]

	reEnd := regexp.MustCompile(`(?s)\r?\nendstream\b`)
	eLoc := reEnd.FindIndex(b[streamDataStart:])
	if eLoc == nil {
		return fmt.Errorf("endstream not found in obj %s", objid)
	}
	streamDataEnd := streamDataStart + eLoc[0]

	head := b[:streamDataStart]
	tail := b[streamDataEnd:] // includes "endstream" and the rest

	var out bytes.Buffer
	out.Write(head)
	out.Write(newData)
	out.Write(tail)

	tmp := qdfIn + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, qdfIn)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// runCmdToFile runs any command, writing its stdout to dest. Non-zero exit is an error.
func runCmdToFile(dest string, name string, args ...string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = f
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// runQpdfCmd treats qpdf exit code 0 or 3 as success.
func runQpdfCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = nil
	cmd.Stdin = nil
	err := cmd.Run()
	if err == nil {
		return nil
	}
	// If qpdf returns status 3 but stderr contains "operation succeeded with warnings", accept.
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 {
		if strings.Contains(strings.ToLower(stderr.String()), "operation succeeded with warnings") {
			return nil
		}
	}
	return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
}

// runQpdfCmdToFile treats qpdf exit code 0 or 3 as success and writes stdout to dest.
func runQpdfCmdToFile(dest string, name string, args ...string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = f
	cmd.Stdin = nil
	err = cmd.Run()
	if err == nil {
		return nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 {
		if strings.Contains(strings.ToLower(stderr.String()), "operation succeeded with warnings") {
			return nil
		}
	}
	return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
}

/* ────────────────────────────────────────────────────────────────────
   Outline/ToC normalization
   ──────────────────────────────────────────────────────────────────── */

// normalizeTOC repacks the file through qpdf so outlines/bookmarks are dropped,
// avoiding source temp-filename entries. Viewers then show plain page numbers.
func normalizeTOC(outPDF string, pageCount int, debug bool) error {
	// If qpdf isn't on PATH, silently skip (non-fatal).
	if _, err := exec.LookPath("qpdf"); err != nil {
		return nil
	}
	tmp := outPDF + ".repacked.pdf"
	// Rebuild from pages only: no outlines/bookmarks carried over.
	// "1-z" means all pages.
	if err := runQpdfCmd("qpdf", "--empty", "--pages", outPDF, "1-z", "--", tmp); err != nil {
		if debug {
			fmt.Printf("[toc] qpdf rebuild failed: %v\n", err)
		}
		return nil
	}
	// Atomic-ish replace
	if err := os.Rename(tmp, outPDF); err != nil {
		_ = copyFile(tmp, outPDF)
		_ = os.Remove(tmp)
	}
	if debug {
		fmt.Printf("[toc] outline cleared; %d pages\n", pageCount)
	}
	return nil
}

/* ────────────────────────────────────────────────────────────────────
   Misc helpers
   ──────────────────────────────────────────────────────────────────── */

func randSuffix() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const n = len(letters)

	// Try crypto/rand first (uniform, safe)
	var rbytes [6]byte
	if _, err := crand.Read(rbytes[:]); err == nil {
		out := make([]byte, 6)
		for i := 0; i < 6; i++ {
			out[i] = letters[int(rbytes[i])%n]
		}
		return string(out)
	}

	// Fallback: LCG based on time, with safe modulus
	out := make([]byte, 6)
	var r uint64 = uint64(time.Now().UnixNano())
	for i := 0; i < 6; i++ {
		r = r*1664525 + 1013904223
		out[i] = letters[int(r%uint64(n))]
	}
	return string(out)
}

func maskPassword(p string) string {
	if len(p) <= 8 {
		return "********"
	}
	return p[:4] + "****" + p[len(p)-4:]
}

package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"html"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pdfcpu "github.com/pdfcpu/pdfcpu/pkg/api"
)

type status struct {
	err     error
	rowNum  int
	pdfPath string
}

func main() {
	//Read command line flags/args
	templateFlag := flag.String("template", "", "path to the html template file")
	inputFlag := flag.String("input", "", "path to the input CSV file")
	excludeFirstFlag := flag.Bool("exclude-first", true, "exclude the first row in the CSV, commonly used for labels")
	mergeOutputFlag := flag.Bool("merge-output", true, "merge all output pdfs into one pdf with multiple pages")
	outputFlag := flag.String("output-dir", "output", "path to the directory to put output PDF files into")
	templateDepsFlag := flag.String("template-dependencies", "", "comma separated string of dependencies for the template")
	flag.Parse()

	//Read input CSV file
	if inputFlag == nil || *inputFlag == "" {
		fmt.Println("Error: Please specify a valid input file")
		os.Exit(1)
	}

	csvRecords := csvReader(*inputFlag)

	if *excludeFirstFlag {
		csvRecords = csvRecords[1:]
	}

	//Read input template file
	templateBytes, err := ioutil.ReadFile(*templateFlag)
	check(err)
	template := string(templateBytes)

	//create temp dir
	_ = os.Mkdir("temp", 0755)
	tempDir, err := filepath.Abs("temp")
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	//link all dependencies in temp dir
	dependencies := strings.Split(*templateDepsFlag, ",")
	for _, path := range dependencies {
		path = strings.TrimSpace(path)
		base := filepath.Base(path)
		os.Link(path, filepath.Join(tempDir, base))
	}

	//create output dir
	_ = os.Mkdir(*outputFlag, 0755)
	outputDir, err := filepath.Abs(*outputFlag)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}

	//fmt.Println("Working dir: ", tempDir)

	generationResult := make(chan status, 1)
	//for each row in input csv file
	for i, row := range csvRecords {
		//call goroutine with template file name, csv row, and output file name
		go generateRecordPDF(template, row, i, tempDir, outputDir, *mergeOutputFlag, generationResult)
	}

	succeeded := make([]int, 0)
	pdfsToMerge := make([]string, 0)

	for range csvRecords {
		result := <-generationResult
		if result.err != nil {
			fmt.Printf("Error processing row %d: ", result.rowNum)
			fmt.Println(result.err)
		} else {
			succeeded = append(succeeded, result.rowNum)
			pdfsToMerge = append(pdfsToMerge, result.pdfPath)
		}
	}

	if *mergeOutputFlag && len(pdfsToMerge) > 0 {
		err = pdfcpu.MergeCreateFile(pdfsToMerge, filepath.Join(outputDir, "output.pdf"), nil)
		if err != nil {
			fmt.Println(err)
		}
	}

}

func generateRecordPDF(templateFile string, rowFields []string, rowNum int, tempDir string, outputDir string, merge bool, retChan chan<- status) {
	//generate array of string pairs to find and replace
	replaceArray := make([]string, len(rowFields)*2)
	for i, val := range rowFields {
		replaceArray[2*i] = "<!--=" + strconv.Itoa(i) + "-->"
		replaceArray[2*i+1] = html.EscapeString(val)
	}

	//replace template placeholders with values
	r := strings.NewReplacer(replaceArray...)
	injectedTemplate := r.Replace(templateFile)

	//write injected template to file
	injectedFile := filepath.Join(tempDir, "injected-template-"+strconv.Itoa(rowNum)+".html")
	//log.Printf(tempDir)
	//log.Printf(injectedFile)
	bytestream := []byte(injectedTemplate)
	err := ioutil.WriteFile(injectedFile, bytestream, 0775)

	if err != nil {
		retChan <- status{err: err, rowNum: rowNum}
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15000*time.Millisecond)
	defer cancel()

	pdfOutputPath := outputDir
	if merge {
		pdfOutputPath = tempDir
	}
	pdfOutputPath = filepath.Join(pdfOutputPath, strconv.Itoa(rowNum)+".pdf")
	//log.Printf(pdfOutputPath)

	cmd := exec.CommandContext(ctx, "chrome/chrome.exe", "--enable-logging", "--disable-extensions", "--headless", "--disable-gpu", "--print-to-pdf-no-header", "--run-all-compositor-stages-before-draw", "--virtual-time-budget=10000", "--print-to-pdf="+pdfOutputPath, injectedFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()

	if err != nil {
		retChan <- status{err: err, rowNum: rowNum}
		log.Fatal(err)
	}

	retChan <- status{err: nil, rowNum: rowNum, pdfPath: pdfOutputPath}
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func csvReader(filename string) [][]string {
	// 1. Open the file
	recordFile, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error reading input file: ", err)
		os.Exit(1)
	} // 2. Initialize the reader
	reader := csv.NewReader(recordFile) // 3. Create reader
	records, err := reader.ReadAll()    // 4. Read all rows in csv
	if err != nil {
		fmt.Println("Error processing input CSV: ", err)
		os.Exit(1)
	}
	return records
}

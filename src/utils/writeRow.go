package utils

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

type RowTransformer func(row []string, idx int) ([]string, error)

func WriteRow(infile *excelize.File, sheetName string, values []interface{}) error {
	rows, err := infile.GetRows(sheetName)
	if err != nil {
		return err
	}

	nextRowIndex := len(rows) + 1
	cellRef, err := excelize.CoordinatesToCellName(1, nextRowIndex)
	if err != nil {
		return err
	}

	return infile.SetSheetRow(sheetName, cellRef, &values)
}

func TransformFile(infile *excelize.File, outfile *excelize.File, forEachRow RowTransformer, sheetName string) error {
	// Get rows iterator for streaming processing
	rows, err := infile.Rows(sheetName)
	if err != nil {
		return err
	}
	defer rows.Close()

	outfile.NewSheet(sheetName)

	rowIndex := 0
	for rows.Next() {
		fmt.Println("Processing", rowIndex)
		// Get current row columns
		row, err := rows.Columns()
		if err != nil {
			return err
		}

		// Transform the row using the provided function
		transformedRow, err := forEachRow(row, rowIndex)
		if err != nil {
			return err
		}

		// Write transformed row to output file
		// Convert to []interface{} for SetSheetRow
		values := make([]interface{}, len(transformedRow))
		for i, v := range transformedRow {
			values[i] = v
		}

		// Set the row in the output file (A1, A2, A3, etc.)
		cellRef, err := excelize.CoordinatesToCellName(1, rowIndex+1)
		if err != nil {
			return err
		}

		err = outfile.SetSheetRow(sheetName, cellRef, &values)
		if err != nil {
			return err
		}

		rowIndex++
	}

	return rows.Error()
}

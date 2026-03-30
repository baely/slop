# Statement Parser

A browser-based tool for bulk parsing ANZ Plus bank statement PDFs and exporting all transaction data as CSV.

## Features

- Drag-and-drop or click to upload multiple PDF statements at once
- Parses ANZ Plus Everyday account statement PDFs
- Extracts all transaction fields: date, description, credit, debit, balance, effective date
- Includes statement metadata: period, BSB, account number, account name
- Smart credit/debit classification using description keywords and balance verification
- Sortable, filterable transaction table
- Export all transactions as a single CSV file
- Runs entirely in the browser (no server-side processing)

## Usage

1. Open the app
2. Drop one or more ANZ Plus statement PDFs onto the drop zone
3. Review parsed transactions in the table
4. Filter or sort as needed
5. Click "Export CSV" to download all transactions

## Deployment

```sh
staticer deploy --domain statements.lab.baileys.dev --expires never
```

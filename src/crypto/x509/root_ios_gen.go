// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

// Generates root_ios.go.
//
// As of iOS 13, there is no API for querying the system trusted X.509 root
// certificates.
//
// Apple publishes the trusted root certificates for iOS and macOS on
// opensource.apple.com so we embed them into the x509 package.
//
// Note that this ignores distrusted and revoked certificates.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

func main() {
	var output = flag.String("output", "root_ios.go", "file name to write")
	var version = flag.String("version", "", "security_certificates version")
	flag.Parse()
	if *version == "" {
		log.Fatal("Select the latest security_certificates version from " +
			"https://opensource.apple.com/source/security_certificates/")
	}

	url := "https://opensource.apple.com/tarballs/security_certificates/security_certificates-%s.tar.gz"
	hc := &http.Client{Timeout: 1 * time.Minute}
	resp, err := hc.Get(fmt.Sprintf(url, *version))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("HTTP status not OK: %s", resp.Status)
	}

	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	defer zr.Close()

	var certs []*x509.Certificate
	pool := x509.NewCertPool()

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		rootsDirectory := fmt.Sprintf("security_certificates-%s/certificates/roots/", *version)
		if dir, file := path.Split(hdr.Name); hdr.Typeflag != tar.TypeReg ||
			dir != rootsDirectory || strings.HasPrefix(file, ".") {
			continue
		}

		der, err := ioutil.ReadAll(tr)
		if err != nil {
			log.Fatal(err)
		}

		c, err := x509.ParseCertificate(der)
		if err != nil {
			log.Printf("Failed to parse certificate %q: %v", hdr.Name, err)
			continue
		}

		certs = append(certs, c)
		pool.AddCert(c)
	}

	// Quick smoke test to check the pool is well formed, and that we didn't end
	// up trusting roots in the removed folder.
	for _, c := range certs {
		if c.Subject.CommonName == "Symantec Class 2 Public Primary Certification Authority - G4" {
			log.Fatal("The pool includes a removed root!")
		}
	}
	conn, err := tls.Dial("tcp", "mail.google.com:443", &tls.Config{
		RootCAs: pool,
	})
	if err != nil {
		log.Fatal(err)
	}
	conn.Close()

	certName := func(c *x509.Certificate) string {
		if c.Subject.CommonName != "" {
			return c.Subject.CommonName
		}
		if len(c.Subject.OrganizationalUnit) > 0 {
			return c.Subject.OrganizationalUnit[0]
		}
		return c.Subject.Organization[0]
	}
	sort.Slice(certs, func(i, j int) bool {
		if strings.ToLower(certName(certs[i])) != strings.ToLower(certName(certs[j])) {
			return strings.ToLower(certName(certs[i])) < strings.ToLower(certName(certs[j]))
		}
		return certs[i].NotBefore.Before(certs[j].NotBefore)
	})

	out := new(bytes.Buffer)
	fmt.Fprintf(out, header, *version)
	fmt.Fprintf(out, "const systemRootsPEM = `\n")

	for _, c := range certs {
		fmt.Fprintf(out, "# %q\n", certName(c))
		h := sha256.Sum256(c.Raw)
		fmt.Fprintf(out, "# % X\n", h[:len(h)/2])
		fmt.Fprintf(out, "# % X\n", h[len(h)/2:])
		b := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: c.Raw,
		}
		if err := pem.Encode(out, b); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Fprintf(out, "`")

	source, err := format.Source(out.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := ioutil.WriteFile(*output, source, 0644); err != nil {
		log.Fatal(err)
	}
}

const header = `// Code generated by root_ios_gen.go -version %s; DO NOT EDIT.
// Update the version in root.go and regenerate with "go generate".

// +build ios
// +build !x509omitbundledroots

package x509

func (c *Certificate) systemVerify(opts *VerifyOptions) (chains [][]*Certificate, err error) {
	return nil, nil
}

// loadSystemRootsWithCgo is not available on iOS.
var loadSystemRootsWithCgo func() (*CertPool, error)

func loadSystemRoots() (*CertPool, error) {
	p := NewCertPool()
	p.AppendCertsFromPEM([]byte(systemRootsPEM))
	return p, nil
}
`

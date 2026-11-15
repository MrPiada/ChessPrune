//go:build !darwin

package main

func newTokenStore() tokenStore {
	path, err := defaultTokenPath()
	if err != nil {
		return errorTokenStore{err: err}
	}
	return fileTokenStore{path: path}
}

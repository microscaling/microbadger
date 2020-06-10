package encryption

// MockService for tests
type MockService struct{}

// make sure it satisfies the interface
var _ Service = (*MockService)(nil)

func NewMockService() MockService {
	return MockService{}
}

// Encrypt on mock returns static data
func (q MockService) Encrypt(input string) (encKey string, encVal string, err error) {
	encKey = `AQEDAHithgxYTcdIpj37aYm1VAycoViFcSM2L+KQ42Aw3R0MdAAAAH4wfAYJKoZIhvcNAQcGoG8wbQIBADBoBgkqhkiG9w0BBwEwHgYJYIZIAWUDBAEuMBEEDMrhUevDKOjuP7L1
XAIBEIA7/F9A1spnmoaehxqU5fi8lBwiZECAvXkSI33YPgJGAsCqmlEAQuirHHp4av4lI7jjvWCIj/nyHxa6Ss8=`
	encVal = "+DKd7lg8HsLD+ISl7ZrP0n6XhmrTRCYDVq4Zj9hTrL1JjxAb2fGsp/2DMSPy"
	err = nil

	return encKey, encVal, err
}

// Decrypt on mock returns static data
func (q MockService) Decrypt(encKey string, envVal string) (result string, err error) {
	result = "Q_Qesb1Z2hA7H94iXu3_buJeQ7416"
	err = nil

	return result, err
}

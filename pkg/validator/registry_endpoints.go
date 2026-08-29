package validator

func publicRegistryEndpoints() Endpoints {
	return Endpoints{
		PyPI: "https://pypi.org/pypi",
		NPM:  "https://registry.npmjs.org",
	}
}

#!/bin/sh
set -eu

mailcloakctl init
mailcloakctl domains add d1.test
mailcloakctl domains add d2.test
mailcloakctl aliases add alias1@d1.test alice
mailcloakctl aliases add alias2@d2.test bob
mailcloakctl apps add app1 "password"
mailcloakctl apps allow app1 app1@d1.test
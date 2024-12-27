# $NAMESPACE will be replaced with the namespace of the test case.

import json
import logging
import requests
import sys
from bs4 import BeautifulSoup

logging.basicConfig(
    level='DEBUG',
    format="%(asctime)s %(levelname)s: %(message)s",
    stream=sys.stdout)

session = requests.Session()

# Click on "Sign In with keycloak" in Spark-history
logging.info("Opening the Spark-history login page")
login_page = session.get("http://sparkhistory-node-default:4180/oauth2/start?rd=%2F")

assert login_page.ok, "Redirection from Spark-history to Keycloak failed"
assert login_page.url.startswith("http://keycloak.$NAMESPACE.svc.cluster.local:8080/realms/kubedoop/protocol/openid-connect/auth?approval_prompt=force&client_id=auth2-proxy"), \
    f"Redirection to the Keycloak login page expected, actual URL: {login_page.url}"

# Enter username and password into the Keycloak login page and click on "Sign In"
logging.info("Logging by oauth2-proxy")
login_page_html = BeautifulSoup(login_page.text, 'html.parser')
authenticate_url = login_page_html.form['action']
welcome_page = session.post(authenticate_url, data={
    'username': "user",
    'password': "password",
})

assert welcome_page.ok, "Login failed"
assert welcome_page.url.rstrip("/") == "http://sparkhistory-node-default:4180", \
    f"Redirection to the Spark-history welcome page expected, actual URL: {welcome_page.url}"

# TODO: sign out from the oauth2-proxy, but sign out from the Keycloak is not possible

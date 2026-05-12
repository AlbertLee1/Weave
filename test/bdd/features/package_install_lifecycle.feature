Feature: Package install lifecycle
  As an ontology administrator I want to install, upgrade, and uninstall
  .weavepkg archives through the public API so the marketplace UI can
  reflect the catalog state and Ontology entities are materialised.

  The feature exercises the three POST/DELETE endpoints in
  cmd/server/routes.go via real chi handlers and asserts triple state:
  HTTP status code + response body + the installed_packages PG row
  reachable through pkg/oms/installedpkgpg, with the ontology import
  side-effect verified via *oms.PGRepository.GetObjectTypeByAPIName.

  Background:
    Given a fresh weave database with migrations applied

  Scenario: Install-BuiltIn registers the package and imports its ontology
    Given a built-in package "bdd-demo" version "1.0.0" targeting ontology "bdd_pkg_install_demo" with object type "widget"
    When the operator installs the built-in package "bdd-demo"
    Then the install response status is 201
    And the install response says version "1.0.0" was applied to ontology "bdd_pkg_install_demo"
    And the installed_packages row "bdd-demo" exists with version "1.0.0" and enabled true
    And the ontology "bdd_pkg_install_demo" has object type "widget"

  Scenario: Update-Version upserts the registry row in place and refreshes ontology
    Given a built-in package "bdd-demo" version "1.0.0" targeting ontology "bdd_pkg_install_demo" with object type "widget"
    And the operator has installed the built-in package "bdd-demo"
    And a built-in package "bdd-demo" version "1.1.0" targeting ontology "bdd_pkg_install_demo" with object type "gadget"
    When the operator installs the built-in package "bdd-demo" with onConflict "skip"
    Then the install response status is 201
    And the installed_packages row "bdd-demo" exists with version "1.1.0" and enabled true
    And the ontology "bdd_pkg_install_demo" has object type "widget"
    And the ontology "bdd_pkg_install_demo" has object type "gadget"
    And exactly 1 installed_packages row exists for name "bdd-demo"

  Scenario: Uninstall removes the registry row but leaves ontology entities intact
    Given a built-in package "bdd-demo" version "1.0.0" targeting ontology "bdd_pkg_install_demo" with object type "widget"
    And the operator has installed the built-in package "bdd-demo"
    When the operator uninstalls the package "bdd-demo"
    Then the uninstall response status is 204
    And no installed_packages row exists for name "bdd-demo"
    And the ontology "bdd_pkg_install_demo" has object type "widget"

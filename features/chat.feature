Feature: Public room chat
  Guests exchange messages in isolated public rooms.

  Scenario: List the available rooms
    Given an isolated chat service
    When I request the room list
    Then the available room IDs are "general,angular,go"

  Scenario: Two guests exchange a message
    Given an isolated chat service
    And " Alice " connects to "general" as "alice"
    And "Bob" connects to "general" as "bob"
    When "alice" sends " hello everyone "
    Then "alice" receives "hello everyone" from "Alice"
    And "bob" receives "hello everyone" from "Alice"

  Scenario: Messages stay within a room
    Given an isolated chat service
    And "Alice" connects to "angular" as "alice"
    And "Bob" connects to "go" as "bob"
    When "alice" sends "Angular question"
    Then "alice" receives "Angular question" from "Alice"
    And "bob" receives no message

  Scenario: A late guest receives room history
    Given an isolated chat service
    And "Alice" connects to "general" as "alice"
    When "alice" sends "Earlier message"
    Then "alice" receives "Earlier message" from "Alice"
    When "Bob" connects to "general" as "bob"
    Then "bob" has history "Earlier message"

  Scenario: A blank message is recoverable
    Given an isolated chat service
    And "Alice" connects to "general" as "alice"
    When "alice" sends "   "
    Then "alice" receives error "invalid_content"
    When "alice" sends "Still connected"
    Then "alice" receives "Still connected" from "Alice"
    And "general" has stored history "Still connected"

  Scenario Outline: Invalid handshakes are rejected before upgrade
    Given an isolated chat service
    When I attempt room "<room>" with name "<name>" and origin ""
    Then the handshake status is <status> with code "<code>"

    Examples:
      | room    | name | status | code         |
      | general |      | 400    | invalid_name |
      | general | abcdefghijklmnopqrstuvwxyz1234567 | 400 | invalid_name |
      |         | Ada  | 400    | invalid_room |
      | unknown | Ada  | 404    | unknown_room |

  Scenario: A disallowed browser origin is rejected
    Given an isolated chat service
    When I attempt room "general" with name "Alice" and origin "https://untrusted.example"
    Then the handshake status is 403 with code "origin_not_allowed"

  Scenario: Disconnect updates room presence
    Given an isolated chat service
    And "Alice" connects to "general" as "alice"
    And "Bob" connects to "general" as "bob"
    When "alice" disconnects
    Then "bob" sees participants "Bob"

  Scenario: Reconnection retrieves missed messages
    Given an isolated chat service
    And "Alice" connects to "general" as "alice"
    And "Bob" connects to "general" as "bob"
    When "alice" disconnects
    And "bob" sends "While you were away"
    Then "bob" receives "While you were away" from "Bob"
    When "Alice" connects to "general" as "alice"
    Then "alice" has history "While you were away"

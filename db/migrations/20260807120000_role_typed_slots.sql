-- migrate:up

-- #626 (task 4): a role's requirement narrows from a capability set to a
-- typed slot. system_role_type is what the role accepts (a component's
-- product's component_type_id must fall within one of these subtrees, per
-- component_type.TypeIsWithin); system_role_product optionally pins the
-- slot to specific products within an accepted type. ON DELETE RESTRICT on
-- both FKs, the same registry-delete-must-not-cascade-through-classified-rows
-- rule component_type's own parent_id and product's component_type_id carry:
-- a role's declaration must not be silently emptied by a catalog delete
-- elsewhere. ON DELETE CASCADE on role_id: withdrawing the role withdraws
-- what it requires, same as system_role_capability today.
CREATE TABLE system_role_type (
    role_id uuid NOT NULL REFERENCES system_role(id) ON DELETE CASCADE,
    component_type_id uuid NOT NULL REFERENCES component_type(id) ON DELETE RESTRICT,
    PRIMARY KEY (role_id, component_type_id)
);
CREATE TABLE system_role_product (
    role_id uuid NOT NULL REFERENCES system_role(id) ON DELETE CASCADE,
    product_id uuid NOT NULL REFERENCES product(id) ON DELETE RESTRICT,
    PRIMARY KEY (role_id, product_id)
);

-- migrate:down
DROP TABLE system_role_product;
DROP TABLE system_role_type;

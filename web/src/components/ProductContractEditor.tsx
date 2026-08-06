import { type JSX } from "solid-js";
import ContractEditor, { type ContractLane } from "./ContractEditor";

// The product arc of the shared ContractEditor, in product language: a product
// declares what every component that is an instance of it exposes. The editor
// itself is classifier-generic (one component serves product, standard, and
// location type) and lane-generic (properties by default, metrics on the metric
// lane); this keeps the product call site reading as what it addresses.
export default function ProductContractEditor(props: { productId: string; official: boolean; lane?: ContractLane }): JSX.Element {
  return <ContractEditor classifier="product" lane={props.lane} id={props.productId} official={props.official} />;
}

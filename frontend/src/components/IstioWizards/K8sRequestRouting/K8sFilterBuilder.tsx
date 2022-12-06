import * as React from 'react';
import {
  Button,
  Dropdown,
  DropdownToggle,
  DropdownItem,
  InputGroup,
  TextInput,
  ButtonVariant
} from '@patternfly/react-core';

type Props = {
  filterType: string;
  filterValue: string;
  headerType: string;
  headerName: string;
  headerValue: string;
  isValid: boolean;
  onSelectFilterType: (filterType: string) => void;
  onHeaderNameChange: (headerName: string) => void;
  onHeaderValueChange: (headerValue: string) => void;
  onSelectHeaderType: (headerType: string) => void;
  onAddFilter: () => void;
};

type State = {
  isFilterDropdown: boolean;
  isOperatorDropdown: boolean;
};

export const REQ_MOD = 'RequestHeaderModifier';
export const RESP_MOD = 'ResponseHeaderModifier';
export const REQ_MIR = 'RequestMirror';
export const REQ_RED = 'RequestRedirect';
export const URL_REW = 'URLRewrite';
export const EXT_REF = 'ExtensionRef';

const filterOptions: string[] = [REQ_MOD, RESP_MOD];

export const SET = 'set';
export const ADD = 'add';
export const REMOVE = 'remove';

const allOptions = {
  [REQ_MOD]: [SET, ADD, REMOVE],
  [RESP_MOD]: [SET, ADD, REMOVE],
};

class K8sFilterBuilder extends React.Component<Props, State> {
  constructor(props) {
    super(props);
    this.state = {
      isFilterDropdown: false,
      isOperatorDropdown: false
    };
  }

  onFilterTypeToggle = () => {
    this.setState({
      isFilterDropdown: !this.state.isFilterDropdown
    });
  };

  onHeaderTypeToggle = () => {
    this.setState({
      isOperatorDropdown: !this.state.isOperatorDropdown
    });
  };

  render() {
    const renderFilterOptions: string[] = allOptions[this.props.filterType]
    console.log("renderFilterOptions " + renderFilterOptions)
    return (
      <InputGroup>
        <Dropdown
          toggle={
            <DropdownToggle onToggle={this.onFilterTypeToggle} data-test={'filtering-type-toggle'}>
              {this.props.filterType}
            </DropdownToggle>
          }
          isOpen={this.state.isFilterDropdown}
          dropdownItems={filterOptions.map((mode, index) => (
            <DropdownItem
              key={mode + '_' + index}
              value={mode}
              component="button"
              onClick={() => {
                this.props.onSelectFilterType(mode);
                this.onFilterTypeToggle();
              }}
              data-test={'filtering-type-' + mode}
            >
              {mode}
            </DropdownItem>
          ))}
        />
        <Dropdown
          toggle={
            <DropdownToggle onToggle={this.onHeaderTypeToggle} data-test={'header-type-toggle'}>
              {this.props.headerType}
            </DropdownToggle>
          }
          dropdownItems={renderFilterOptions.map((op, index) => (
            <DropdownItem
              key={op + '_' + index}
              value={op}
              component="button"
              onClick={() => {
                this.props.onSelectHeaderType(op);
                this.onHeaderTypeToggle();
              }}
              data-test={'header-type-' + op}
            >
              {op}
            </DropdownItem>
          ))}
        />
        {(this.props.filterType === REQ_MOD || this.props.filterType === RESP_MOD) && (
          <TextInput
            id="header-name-id"
            value={this.props.headerName}
            onChange={this.props.onHeaderNameChange}
            placeholder="Header name..."
          />
        )}
        {(this.props.filterType === REQ_MOD || this.props.filterType === RESP_MOD) && this.props.headerType !== REMOVE && (
          <TextInput
            id="header-value-id"
            value={this.props.headerValue}
            onChange={this.props.onHeaderValueChange}
            placeholder="Header Value..."
          />
        )}
        <Button
          variant={ButtonVariant.secondary}
          disabled={!this.props.isValid}
          onClick={this.props.onAddFilter}
          data-test="add-Filter"
        >
          Add Filter
        </Button>
      </InputGroup>
    );
  }
}

export default K8sFilterBuilder;

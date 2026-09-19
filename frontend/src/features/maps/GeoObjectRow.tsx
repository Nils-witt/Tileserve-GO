import { Button, Checkbox, Stack, TableCell, TableRow } from '@mui/material';
import type { GeoObject } from '../../api/types';

export default function GeoObjectRow({
  obj,
  selected,
  onToggleSelect,
  onEdit,
  onDelete,
}: {
  obj: GeoObject;
  selected: boolean;
  onToggleSelect: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <TableRow>
      <TableCell align="center" padding="checkbox">
        <Checkbox checked={selected} onChange={onToggleSelect} />
      </TableCell>
      <TableCell>{obj.name}</TableCell>
      <TableCell>{obj.externalId}</TableCell>
      <TableCell>{obj.latitude}</TableCell>
      <TableCell>{obj.longitude}</TableCell>
      <TableCell>{obj.street}</TableCell>
      <TableCell>{obj.housenumber}</TableCell>
      <TableCell>{obj.postcode}</TableCell>
      <TableCell>{obj.city}</TableCell>
      <TableCell>{obj.cityDistrict}</TableCell>
      <TableCell sx={{ minWidth: 140 }}>
        <Stack direction="row" spacing={1}>
          <Button size="small" onClick={onEdit}>
            Edit
          </Button>
          <Button size="small" color="error" onClick={onDelete}>
            Delete
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
  );
}
